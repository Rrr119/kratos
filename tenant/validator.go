// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
	"github.com/tidwall/sjson"

	"github.com/ory/herodot"
	"github.com/ory/kratos/identity"
	"github.com/ory/kratos/schema"
)

// IdentityValidationDependencies is the subset of the registry the
// validator needs. It intentionally mirrors the interfaces used by
// identity.Validator so a registry can satisfy both.
type IdentityValidationDependencies interface {
	Provider
	schema.IdentitySchemaProvider
	IdentityValidator() *identity.Validator
}

// IdentityValidator validates identity traits against the effective
// schema of the tenant the identity belongs to.
//
// It wraps identity.Validator rather than replacing it: schema
// extensions (credentials, verification, recovery hooks), phone
// normalization, and error formatting all stay in the existing code
// path. This type only resolves which schema document applies after
// tenant inheritance, and makes sure the identity's SchemaID points at
// a schema that belongs to the tenant.
type IdentityValidator struct {
	deps            IdentityValidationDependencies
	resolver        *SchemaResolver
	traitsValidator *TraitsValidator
}

// NewIdentityValidator builds a tenant-aware validator.
func NewIdentityValidator(deps IdentityValidationDependencies, resolver *SchemaResolver) *IdentityValidator {
	return &IdentityValidator{
		deps:            deps,
		resolver:        resolver,
		traitsValidator: NewTraitsValidator(resolver),
	}
}

// ResolveSchemaID maps a tenant id to the identity schema id that must
// be used for validation. An unknown tenant falls back to the identity's
// own schema so pre-multi-tenant callers keep working; an explicitly
// configured tenant overrides the identity's schema id.
func (v *IdentityValidator) ResolveSchemaID(ctx context.Context, tenantID string, i *identity.Identity) (string, error) {
	configs, err := v.deps.TenantConfigs(ctx)
	if err != nil {
		return "", err
	}

	if t, err := configs.FindByID(tenantID); err == nil && t.SchemaID != "" {
		schemas, err := v.deps.IdentityTraitsSchemas(ctx)
		if err != nil {
			return "", err
		}
		if _, err := schemas.GetByID(t.SchemaID); err != nil {
			return "", err
		}
		return t.SchemaID, nil
	}

	if i.SchemaID != "" {
		return i.SchemaID, nil
	}
	schemas, err := v.deps.IdentityTraitsSchemas(ctx)
	if err != nil {
		return "", err
	}
	first := schemas.List(0, 1)
	if len(first) == 0 {
		return "", errors.WithStack(herodot.ErrMisconfiguration().WithReason("No identity schema is configured."))
	}
	return first[0].ID, nil
}

// EffectiveSchemaDocument returns the inherited, merged schema document
// for the tenant's schema as raw JSON bytes.
func (v *IdentityValidator) EffectiveSchemaDocument(schemaID string) ([]byte, error) {
	return v.resolver.ResolveBytes(schemaID)
}

// Validate runs the full identity validation for i in the scope of
// tenantID. It:

//  1. Resolves the applicable schema id from the tenant configuration.
//  2. Pins the identity's SchemaID to that schema so the downstream
//     extensions and error pointers use the same document.
//  3. Delegates to the standard identity.Validator, which compiles the
//     schema and enforces field types, required fields, and extensions.
//
// Inheritance is applied at schema-load time by the tenant SchemaSource,
// so no validation behavior changes for tenants that do not extend
// anything.
func (v *IdentityValidator) Validate(ctx context.Context, tenantID string, i *identity.Identity) error {
	schemaID, err := v.ResolveSchemaID(ctx, tenantID, i)
	if err != nil {
		return err
	}
	i.SchemaID = schemaID

	if len(i.Traits) == 0 {
		i.Traits = []byte(`{}`)
	}

	// Re-wrap traits under the `traits` root exactly like
	// identity.Validator.ValidateWithRunner does, so instance pointers in
	// validation errors stay identical to the non-tenant path.
	if _, err := sjson.SetRawBytes([]byte(`{}`), "traits", i.Traits); err != nil {
		return errors.WithStack(herodot.ErrBadRequest().WithError(err.Error()))
	}

	return v.deps.IdentityValidator().Validate(ctx, i)
}

// ValidateTraits validates an arbitrary traits document (for example a
// login submission before an identity row exists) against the tenant's
// effective, inherited schema.
func (v *IdentityValidator) ValidateTraits(ctx context.Context, tenantID string, traits json.RawMessage) error {
	schemaID, err := v.ResolveSchemaID(ctx, tenantID, &identity.Identity{})
	if err != nil {
		return err
	}
	return v.traitsValidator.ValidateTraits(ctx, schemaID, traits)
}

// ValidateRequestTraits is the high-level entry point used by
// self-service strategies to field-validate a login or registration
// submission. It resolves the tenant from the request headers/query and
// validates the traits against the tenant's effective, inherited schema.
//
// When no tenants are configured the call degrades to validating against
// the default identity schema via this same validator, so single-tenant
// deployments keep working without strategy-specific branching.
func (v *IdentityValidator) ValidateRequestTraits(r *http.Request, traits json.RawMessage) error {
	ctx := r.Context()
	configs, err := v.deps.TenantConfigs(ctx)
	if err != nil {
		return err
	}

	tenantID := DefaultID
	if requested := TenantFromRequest(r); requested != "" {
		if len(configs) == 0 {
			// Multi-tenancy is not enabled: ignore the header rather than
			// rejecting traffic from clients that send it preemptively.
			tenantID = DefaultID
		} else {
			if _, err := configs.FindByID(requested); err != nil {
				return err
			}
			tenantID = requested
		}
	}
	return v.ValidateTraits(ctx, tenantID, traits)
}
