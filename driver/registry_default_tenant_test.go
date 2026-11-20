// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package driver_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/driver/config"
	"github.com/ory/kratos/pkg"
	"github.com/ory/kratos/tenant"
	"github.com/ory/x/sqlxx"
)

func base64Schema(t *testing.T, doc string) string {
	t.Helper()
	return "base64://" + base64.StdEncoding.EncodeToString([]byte(doc))
}

// TestRegistryDefault_TenantIdentityValidation drives the full registry
// wiring: tenant config -> schema inheritance resolver -> tenant-aware
// trait validation, including the single-tenant fallback path.
func TestRegistryDefault_TenantIdentityValidation(t *testing.T) {
	ctx := context.Background()
	conf, reg := pkg.NewFastRegistryWithMocks(t)

	parentSchema := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {
			"traits": {
				"type": "object",
				"properties": {
					"email": {"type": "string", "format": "email"}
				},
				"required": ["email"]
			}
		}
	}`
	childSchema := `{
		"extends": "base",
		"type": "object",
		"properties": {
			"traits": {
				"type": "object",
				"properties": {
					"org": {"type": "string", "minLength": 2}
				},
				"required": ["org"]
			}
		}
	}`

	conf.MustSet(ctx, config.ViperKeyIdentitySchemas, []config.Schema{
		{ID: "default", URL: base64Schema(t, parentSchema)},
		{ID: "base", URL: base64Schema(t, parentSchema)},
		{ID: "strict", URL: base64Schema(t, childSchema)},
	})
	conf.MustSet(ctx, config.ViperKeyTenants, []config.Tenant{
		{
			ID:       "acme",
			Name:     "Acme",
			SchemaID: "strict",
			RecoveryChannels: []config.TenantRecoveryChannel{
				{ID: "mail", Type: "email", Enabled: true},
				{ID: "text", Type: "sms", Enabled: false},
			},
		},
	})

	// The registry exposes the tenant contract.
	provider, ok := any(reg).(tenant.Provider)
	require.True(t, ok, "registry must implement tenant.Provider")
	configs, err := provider.TenantConfigs(ctx)
	require.NoError(t, err)
	require.Len(t, configs, 1)

	validator := reg.TenantIdentityValidator()

	requestWithTenant := func(id string) *http.Request {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, "/self-service/login", nil)
		require.NoError(t, err)
		if id != "" {
			r.Header.Set(tenant.HeaderName, id)
		}
		return r
	}

	t.Run("tenant schema enforces inherited plus child required fields", func(t *testing.T) {
		err := validator.ValidateRequestTraits(requestWithTenant("acme"), []byte(`{}`))
		require.Error(t, err)

		err = validator.ValidateRequestTraits(requestWithTenant("acme"),
			[]byte(`{"email":"a@b.com","org":"acme"}`))
		require.NoError(t, err)
	})

	t.Run("unknown tenant is rejected", func(t *testing.T) {
		err := validator.ValidateRequestTraits(requestWithTenant("ghost"),
			[]byte(`{"email":"a@b.com"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("request without tenant uses the default schema", func(t *testing.T) {
		// The default schema only requires email; org is not constrained.
		err := validator.ValidateRequestTraits(requestWithTenant(""),
			[]byte(`{"email":"a@b.com"}`))
		require.NoError(t, err)
	})

	t.Run("recovery channel policy is reachable from the registry", func(t *testing.T) {
		resolver := tenant.NewRecoveryChannelResolver(provider)
		flow := &stubRecoveryFlow{}
		require.NoError(t, tenant.BindTenant(flow, "acme"))

		require.NoError(t, resolver.EnsureChannelAllowed(ctx, flow, tenant.RecoveryChannelEmail))
		err := resolver.EnsureChannelAllowed(ctx, flow, tenant.RecoveryChannelSMS)
		require.Error(t, err)
	})
}

type stubRecoveryFlow struct {
	internal sqlxx.JSONRawMessage
}

func (s *stubRecoveryFlow) EnsureInternalContext() {
	if len(s.internal) == 0 {
		s.internal = sqlxx.JSONRawMessage(`{}`)
	}
}

func (s *stubRecoveryFlow) GetInternalContext() sqlxx.JSONRawMessage    { return s.internal }
func (s *stubRecoveryFlow) SetInternalContext(raw sqlxx.JSONRawMessage) { s.internal = raw }
