// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/identity"
	"github.com/ory/kratos/session"
	"github.com/ory/kratos/tenant"
)

func TestSessionCompatibility_TenantRoundTrip(t *testing.T) {
	t.Parallel()

	compat := tenant.NewSessionCompatibility()
	s := &session.Session{Identity: &identity.Identity{}}

	// Legacy session without any tenant metadata resolves to default.
	assert.Equal(t, tenant.DefaultID, compat.TenantOf(s))

	require.NoError(t, compat.SetTenant(s, "acme"))
	assert.Equal(t, "acme", compat.TenantOf(s))

	// The tenant is persisted inside existing public metadata, so no new
	// storage column is required.
	assert.JSONEq(t, `{"tenant_id":"acme"}`, string(s.Identity.MetadataPublic))
}

func TestSessionCompatibility_PreservesExistingMetadata(t *testing.T) {
	t.Parallel()

	compat := tenant.NewSessionCompatibility()
	s := &session.Session{
		Identity: &identity.Identity{
			MetadataPublic: []byte(`{"display_name":"Alice"}`),
		},
	}

	require.NoError(t, compat.SetTenant(s, "globex"))
	assert.JSONEq(t, `{"display_name":"Alice","tenant_id":"globex"}`,
		string(s.Identity.MetadataPublic))
}

func TestSessionCompatibility_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	compat := tenant.NewSessionCompatibility()
	original := &session.Session{
		Active:    true,
		ExpiresAt: time.Now().Add(time.Hour),
		Identity:  &identity.Identity{SchemaID: "default", State: identity.StateActive},
	}
	require.NoError(t, compat.SetTenant(original, "acme"))

	encoded, err := compat.MarshalSession(original)
	require.NoError(t, err)

	// The tenant surfaces as a top-level claim for API consumers.
	var top struct {
		TenantID string `json:"tenant_id"`
		Active   bool   `json:"active"`
	}
	require.NoError(t, json.Unmarshal(encoded, &top))
	assert.Equal(t, "acme", top.TenantID)
	assert.True(t, top.Active)

	// Round-trip through a fresh session value.
	decoded := &session.Session{}
	require.NoError(t, compat.UnmarshalSession(encoded, decoded))
	assert.Equal(t, "acme", compat.TenantOf(decoded))
}

func TestSessionCompatibility_LegacyPayloadWithoutClaim(t *testing.T) {
	t.Parallel()

	compat := tenant.NewSessionCompatibility()

	// A payload emitted by an older Kratos version has no tenant_id claim
	// but must still decode and resolve to the default tenant.
	legacy, err := json.Marshal(&session.Session{
		Active:    true,
		ExpiresAt: time.Now().Add(time.Hour),
		Identity:  &identity.Identity{State: identity.StateActive},
	})
	require.NoError(t, err)

	decoded := &session.Session{}
	require.NoError(t, compat.UnmarshalSession(legacy, decoded))
	assert.True(t, decoded.Active)
	assert.Equal(t, tenant.DefaultID, compat.TenantOf(decoded))
}

func TestSessionCompatibility_TokenClaims(t *testing.T) {
	t.Parallel()

	compat := tenant.NewSessionCompatibility()
	s := &session.Session{Identity: &identity.Identity{}}
	require.NoError(t, compat.SetTenant(s, "acme"))

	claims := compat.SessionClaims(s)
	assert.Equal(t, "acme", claims[tenant.SessionClaim])
}
