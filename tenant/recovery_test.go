// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/tenant"
	"github.com/ory/x/sqlxx"
)

// fakeRecoveryFlow implements tenant.RecoveryFlow for tests.
type fakeRecoveryFlow struct {
	internal sqlxx.JSONRawMessage
}

func (f *fakeRecoveryFlow) EnsureInternalContext() {
	if len(f.internal) == 0 {
		f.internal = sqlxx.JSONRawMessage(`{}`)
	}
}

func (f *fakeRecoveryFlow) GetInternalContext() sqlxx.JSONRawMessage    { return f.internal }
func (f *fakeRecoveryFlow) SetInternalContext(raw sqlxx.JSONRawMessage) { f.internal = raw }

// staticProvider returns a fixed tenant configuration set.
type staticProvider struct{ configs tenant.Configs }

func (s staticProvider) TenantConfigs(_ context.Context) (tenant.Configs, error) {
	return s.configs, nil
}

func TestTenantFromRequest(t *testing.T) {
	t.Parallel()

	t.Run("header wins", func(t *testing.T) {
		r := &http.Request{
			Header: http.Header{tenant.HeaderName: []string{"acme"}},
			URL:    &url.URL{RawQuery: "tenant=other"},
		}
		assert.Equal(t, "acme", tenant.TenantFromRequest(r))
	})

	t.Run("query fallback", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: "tenant=globex"}}
		assert.Equal(t, "globex", tenant.TenantFromRequest(r))
	})

	t.Run("empty means default", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{}}
		assert.Equal(t, "", tenant.TenantFromRequest(r))
	})
}

func TestRecoveryFlowTenantBinding(t *testing.T) {
	t.Parallel()

	f := &fakeRecoveryFlow{}
	require.NoError(t, tenant.BindTenant(f, "acme"))
	assert.Equal(t, "acme", tenant.TenantFromFlow(f))

	// An unbound flow resolves to the default tenant, mirroring legacy
	// flows created before multi-tenancy existed.
	legacy := &fakeRecoveryFlow{}
	assert.Equal(t, tenant.DefaultID, tenant.TenantFromFlow(legacy))
}

func TestRecoveryChannelResolver(t *testing.T) {
	t.Parallel()

	provider := staticProvider{configs: tenant.Configs{
		{
			ID: "email-only",
			RecoveryChannels: []tenant.Channel{
				{ID: "mail", Type: tenant.RecoveryChannelEmail, Enabled: true},
				{ID: "text", Type: tenant.RecoveryChannelSMS, Enabled: false},
			},
		},
		{
			ID:               "unrestricted",
			RecoveryChannels: nil,
		},
		tenant.Config{ID: tenant.DefaultID},
	}}

	resolver := tenant.NewRecoveryChannelResolver(provider)
	ctx := context.Background()

	t.Run("tenant with explicit allow list", func(t *testing.T) {
		f := &fakeRecoveryFlow{}
		require.NoError(t, tenant.BindTenant(f, "email-only"))

		require.NoError(t, resolver.EnsureChannelAllowed(ctx, f, tenant.RecoveryChannelEmail))
		err := resolver.EnsureChannelAllowed(ctx, f, tenant.RecoveryChannelSMS)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed for this tenant")

		channels, err := resolver.ChannelsFor(ctx, f)
		require.NoError(t, err)
		require.Len(t, channels, 1)
		assert.Equal(t, tenant.RecoveryChannelEmail, channels[0].Type)
	})

	t.Run("tenant without channel policy allows everything", func(t *testing.T) {
		f := &fakeRecoveryFlow{}
		require.NoError(t, tenant.BindTenant(f, "unrestricted"))

		require.NoError(t, resolver.EnsureChannelAllowed(ctx, f, tenant.RecoveryChannelEmail))
		require.NoError(t, resolver.EnsureChannelAllowed(ctx, f, tenant.RecoveryChannelSMS))

		// nil means "no restriction", distinct from an explicit empty list.
		channels, err := resolver.ChannelsFor(ctx, f)
		require.NoError(t, err)
		assert.Nil(t, channels)
	})

	t.Run("unknown tenant is rejected", func(t *testing.T) {
		f := &fakeRecoveryFlow{}
		require.NoError(t, tenant.BindTenant(f, "ghost"))
		err := resolver.EnsureChannelAllowed(ctx, f, tenant.RecoveryChannelEmail)
		require.Error(t, err)
	})
}

func TestInferChannel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, tenant.RecoveryChannelEmail, tenant.InferChannel("user@example.com"))
	assert.Equal(t, tenant.RecoveryChannelSMS, tenant.InferChannel("+15551234567"))
	assert.Equal(t, tenant.RecoveryChannelSMS, tenant.InferChannel("not-an-email"))
}

func TestValidateTenant(t *testing.T) {
	t.Parallel()

	provider := staticProvider{configs: tenant.Configs{{ID: "acme"}}}

	require.NoError(t, tenant.ValidateTenant(context.Background(), provider, "acme"))
	require.Error(t, tenant.ValidateTenant(context.Background(), provider, "unknown"))

	// With no tenants configured the deployment is single-tenant and any
	// value is accepted.
	empty := staticProvider{}
	require.NoError(t, tenant.ValidateTenant(context.Background(), empty, "anything"))
}

func TestBindTenantFromRequest(t *testing.T) {
	t.Parallel()

	provider := staticProvider{configs: tenant.Configs{
		{ID: tenant.DefaultID},
		{ID: "acme"},
	}}

	t.Run("binds header tenant when configured", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest("GET", "/self-service/recovery/api", nil)
		r.Header.Set(tenant.HeaderName, "acme")
		f := &fakeRecoveryFlow{}

		require.NoError(t, tenant.BindTenantFromRequest(provider, r, f))
		assert.Equal(t, "acme", tenant.TenantFromFlow(f))
	})

	t.Run("rejects unknown tenant", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest("GET", "/self-service/recovery/api", nil)
		r.Header.Set(tenant.HeaderName, "ghost")
		f := &fakeRecoveryFlow{}

		require.Error(t, tenant.BindTenantFromRequest(provider, r, f))
	})

	t.Run("no tenant header defaults", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest("GET", "/self-service/recovery/api", nil)
		f := &fakeRecoveryFlow{}

		require.NoError(t, tenant.BindTenantFromRequest(provider, r, f))
		assert.Equal(t, tenant.DefaultID, tenant.TenantFromFlow(f))
	})

	t.Run("registry without provider is a no-op", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest("GET", "/self-service/recovery/api", nil)
		f := &fakeRecoveryFlow{}

		require.NoError(t, tenant.BindTenantFromRequest(struct{}{}, r, f))
		assert.Equal(t, tenant.DefaultID, tenant.TenantFromFlow(f))
	})

	t.Run("empty tenant config is single-tenant no-op", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest("GET", "/self-service/recovery/api", nil)
		r.Header.Set(tenant.HeaderName, "acme")
		f := &fakeRecoveryFlow{}

		require.NoError(t, tenant.BindTenantFromRequest(staticProvider{}, r, f))
		assert.Equal(t, tenant.DefaultID, tenant.TenantFromFlow(f))
	})
}

func TestEnsureRecoveryChannel(t *testing.T) {
	t.Parallel()

	provider := staticProvider{configs: tenant.Configs{
		{
			ID: "email-only",
			RecoveryChannels: []tenant.Channel{
				{ID: "mail", Type: tenant.RecoveryChannelEmail, Enabled: true},
			},
		},
	}}
	ctx := context.Background()

	t.Run("allowed channel passes", func(t *testing.T) {
		t.Parallel()
		f := &fakeRecoveryFlow{}
		require.NoError(t, tenant.BindTenant(f, "email-only"))
		require.NoError(t, tenant.EnsureRecoveryChannel(ctx, provider, f, tenant.RecoveryChannelEmail))
	})

	t.Run("disallowed channel fails", func(t *testing.T) {
		t.Parallel()
		f := &fakeRecoveryFlow{}
		require.NoError(t, tenant.BindTenant(f, "email-only"))
		require.Error(t, tenant.EnsureRecoveryChannel(ctx, provider, f, tenant.RecoveryChannelSMS))
	})

	t.Run("registry without provider is a no-op", func(t *testing.T) {
		t.Parallel()
		f := &fakeRecoveryFlow{}
		require.NoError(t, tenant.EnsureRecoveryChannel(ctx, struct{}{}, f, tenant.RecoveryChannelSMS))
	})

	t.Run("empty config is a no-op", func(t *testing.T) {
		t.Parallel()
		f := &fakeRecoveryFlow{}
		require.NoError(t, tenant.EnsureRecoveryChannel(ctx, staticProvider{}, f, tenant.RecoveryChannelSMS))
	})
}
