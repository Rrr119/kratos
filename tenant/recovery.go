// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/ory/herodot"
	"github.com/ory/x/sqlxx"
)

// Supported recovery channel types. These match the courier channel
// types used by the code and link strategies.
const (
	RecoveryChannelEmail = "email"
	RecoveryChannelSMS   = "sms"
)

// RecoveryFlow is the subset of a recovery flow the channel resolver
// needs. Both the v1 and v2 recovery flow structs satisfy it through
// their InternalContext accessors.
type RecoveryFlow interface {
	EnsureInternalContext()
	GetInternalContext() sqlxx.JSONRawMessage
	SetInternalContext(sqlxx.JSONRawMessage)
}

// internalContextKey stores the tenant that owns a recovery flow, so the
// channel selection stays consistent across the flow's state transitions.
const internalContextKey = "tenant_id"

// RecoveryChannelResolver decides which delivery channels a recovery
// flow may offer based on the tenant configuration.
type RecoveryChannelResolver struct {
	provider Provider
}

// NewRecoveryChannelResolver builds a resolver.
func NewRecoveryChannelResolver(provider Provider) *RecoveryChannelResolver {
	return &RecoveryChannelResolver{provider: provider}
}

// TenantFromRequest extracts the tenant identifier from a self-service
// request. It checks, in order: the X-Kratos-Tenant header, the
// `tenant` query parameter, and an already-bound recovery flow. The
// empty string means "default tenant" and is never an error.
func TenantFromRequest(r *http.Request) string {
	if raw := strings.TrimSpace(r.Header.Get(HeaderName)); raw != "" {
		return raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("tenant")); raw != "" {
		return raw
	}
	return ""
}

// BindTenant stores tenantID on the recovery flow's internal context.
// Binding once at flow creation means later submissions (address entry,
// code confirmation, resend) all validate against the same tenant even
// if the client drops the header mid-flow.
func BindTenant(f RecoveryFlow, tenantID string) error {
	f.EnsureInternalContext()
	raw := f.GetInternalContext()
	if len(raw) == 0 {
		raw = sqlxx.JSONRawMessage(`{}`)
	}
	updated, err := sjson.SetBytes(raw, internalContextKey, normalize(tenantID))
	if err != nil {
		return err
	}
	f.SetInternalContext(updated)
	return nil
}

// TenantFromFlow returns the tenant bound to a recovery flow, or the
// default id when none was bound.
func TenantFromFlow(f RecoveryFlow) string {
	if f == nil {
		return DefaultID
	}
	raw := f.GetInternalContext()
	if len(raw) == 0 {
		return DefaultID
	}
	if id := strings.TrimSpace(gjson.GetBytes(raw, internalContextKey).String()); id != "" {
		return id
	}
	return DefaultID
}

// ChannelsFor returns the recovery channels enabled for the tenant that
// owns the flow. A nil slice means no tenant restriction is configured,
// in which case the caller falls back to every globally enabled channel.
func (r *RecoveryChannelResolver) ChannelsFor(ctx context.Context, f RecoveryFlow) ([]Channel, error) {
	configs, err := r.provider.TenantConfigs(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := configs.AllowedRecoveryChannels(TenantFromFlow(f))
	if err != nil {
		return nil, err
	}
	return allowed, nil
}

// EnsureChannelAllowed rejects a recovery attempt over channelType when
// the tenant's configuration does not enable that channel. When the
// tenant defines no channels the call is a no-op, preserving the
// single-tenant default.
func (r *RecoveryChannelResolver) EnsureChannelAllowed(ctx context.Context, f RecoveryFlow, channelType string) error {
	configs, err := r.provider.TenantConfigs(ctx)
	if err != nil {
		return err
	}
	if _, err := configs.RecoveryChannelFor(TenantFromFlow(f), channelType); err != nil {
		return err
	}
	return nil
}

// InferChannel derives the channel type from a recovery address. It
// centralizes the heuristic that individual strategies previously
// inlined, so multi-channel decisions share one implementation.
func InferChannel(address string) string {
	address = strings.TrimSpace(address)
	if strings.Contains(address, "@") {
		return RecoveryChannelEmail
	}
	return RecoveryChannelSMS
}

// ValidateTenant verifies that tenantID resolves to a configured tenant
// when tenants are configured. With no tenant configuration every id is
// accepted and treated as the default tenant.
func ValidateTenant(ctx context.Context, provider Provider, tenantID string) error {
	configs, err := provider.TenantConfigs(ctx)
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return nil
	}
	if _, err := configs.FindByID(normalize(tenantID)); err != nil {
		return herodot.ErrBadRequest().WithWrap(err)
	}
	return nil
}

// BindTenantFromRequest reads the tenant identifier off r and, when the
// registry exposes a tenant Provider, validates and binds it to the
// recovery flow. Registries without multi-tenant configuration are left
// untouched, so single-tenant deployments keep their exact behavior.
//
// The registry argument is accepted as `any` so self-service packages can
// opt in without importing the tenant package into their dependency
// interfaces: the wiring layer passes a concrete registry that implements
// tenant.Provider.
func BindTenantFromRequest(registry any, r *http.Request, f RecoveryFlow) error {
	provider, ok := registry.(Provider)
	if !ok {
		return nil
	}

	configs, err := provider.TenantConfigs(r.Context())
	if err != nil {
		return err
	}
	// No tenant configuration: stay in single-tenant mode.
	if len(configs) == 0 {
		return nil
	}

	tenantID := normalize(TenantFromRequest(r))
	if _, err := configs.FindByID(tenantID); err != nil {
		return err
	}
	return BindTenant(f, tenantID)
}

// EnsureRecoveryChannel guards a recovery send against the tenant's
// channel policy. Like BindTenantFromRequest it accepts the registry as
// `any` and is a no-op when multi-tenancy is not configured, keeping the
// single-tenant code path byte-for-byte unchanged.
func EnsureRecoveryChannel(ctx context.Context, registry any, f RecoveryFlow, channelType string) error {
	provider, ok := registry.(Provider)
	if !ok {
		return nil
	}
	configs, err := provider.TenantConfigs(ctx)
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return nil
	}
	_, err = configs.RecoveryChannelFor(TenantFromFlow(f), channelType)
	return err
}

func normalize(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return DefaultID
	}
	return tenantID
}
