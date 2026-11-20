// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/identity"
	"github.com/ory/kratos/schema"
	"github.com/ory/kratos/tenant"
)

// stubDeps satisfies tenant.IdentityValidationDependencies for the
// validator unit tests without standing up a full registry.
type stubDeps struct {
	configs tenant.Configs
	schemas schema.Schemas
}

func (s *stubDeps) TenantConfigs(_ context.Context) (tenant.Configs, error) {
	return s.configs, nil
}

func (s *stubDeps) IdentityTraitsSchemas(_ context.Context) (schema.IdentitySchemaList, error) {
	return s.schemas, nil
}

func (s *stubDeps) IdentityValidator() *identity.Validator {
	// The traits-only tests compile the merged schema directly and never
	// invoke the wrapped identity validator.
	return nil
}

func TestIdentityValidator_ResolveSchemaID(t *testing.T) {
	t.Parallel()

	deps := &stubDeps{
		configs: tenant.Configs{
			{ID: "acme", SchemaID: "strict"},
			{ID: tenant.DefaultID, SchemaID: "default"},
		},
		schemas: schema.Schemas{
			{ID: "default"},
			{ID: "strict"},
		},
	}
	resolver := tenant.NewSchemaResolver(memorySchemaSource{})
	validator := tenant.NewIdentityValidator(deps, resolver)
	ctx := context.Background()

	got, err := validator.ResolveSchemaID(ctx, "acme", &identity.Identity{})
	require.NoError(t, err)
	assert.Equal(t, "strict", got)

	// Unknown tenant falls back to the identity's own schema id.
	got, err = validator.ResolveSchemaID(ctx, "ghost", &identity.Identity{SchemaID: "default"})
	require.NoError(t, err)
	assert.Equal(t, "default", got)
}

func TestIdentityValidator_RequestRejectsUnknownTenant(t *testing.T) {
	t.Parallel()

	deps := &stubDeps{
		configs: tenant.Configs{{ID: "acme", SchemaID: "default"}},
		schemas: schema.Schemas{{ID: "default"}},
	}
	resolver := tenant.NewSchemaResolver(memorySchemaSource{
		"default": mustDoc(t, `{"type":"object"}`),
	})
	validator := tenant.NewIdentityValidator(deps, resolver)

	req := httptest.NewRequest("POST", "/self-service/login", nil)
	req.Header.Set(tenant.HeaderName, "ghost")

	err := validator.ValidateRequestTraits(req, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestIdentityValidator_EffectiveSchemaDocument(t *testing.T) {
	t.Parallel()

	source := memorySchemaSource{
		"base":  mustDoc(t, `{"type":"object","title":"base"}`),
		"child": mustDoc(t, `{"extends":"base","title":"child"}`),
	}
	deps := &stubDeps{schemas: schema.Schemas{{ID: "child"}}}
	validator := tenant.NewIdentityValidator(deps, tenant.NewSchemaResolver(source))

	raw, err := validator.EffectiveSchemaDocument("child")
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.NotContains(t, decoded, "extends")
	assert.Equal(t, "child", decoded["title"])
	assert.Equal(t, "object", decoded["type"])
}
