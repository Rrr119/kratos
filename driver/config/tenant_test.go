// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/driver/config"
	"github.com/ory/kratos/tenant"
	"github.com/ory/x/configx"
	"github.com/ory/x/contextx"
	"github.com/ory/x/logrusx"
)

func newTenantConfig(t *testing.T, values map[string]any) *config.Config {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return config.MustNew(t, logrusx.New("", ""), &contextx.Default{},
		configx.SkipValidation(),
		configx.WithValues(values),
		configx.WithContext(ctx),
	)
}

func emailChannel() map[string]any {
	return map[string]any{"id": "mail", "type": "email", "enabled": true}
}

func smsChannel(enabled bool) map[string]any {
	return map[string]any{"id": "text", "type": "sms", "enabled": enabled}
}

// TestTenantConfigs_ThreeScenarios exercises the three tenant
// configurations operators are expected to deploy:
//
//  1. A schema-pinning tenant that selects a stricter identity schema.
//  2. A channel-restricted tenant that limits recovery to email.
//  3. An unrestricted tenant that inherits global defaults.
func TestTenantConfigs_ThreeScenarios(t *testing.T) {
	t.Parallel()

	cfg := newTenantConfig(t, map[string]any{
		config.ViperKeyTenants: []any{
			map[string]any{
				"id":                "tenant-strict",
				"name":              "Strict Tenant",
				"schema_id":         "employee",
				"recovery_channels": []any{emailChannel()},
			},
			map[string]any{
				"id":        "tenant-email-only",
				"name":      "Email Only Tenant",
				"schema_id": "customer",
				"recovery_channels": []any{
					emailChannel(),
					smsChannel(false),
				},
			},
			map[string]any{
				"id":        "tenant-open",
				"name":      "Open Tenant",
				"schema_id": "partner",
			},
		},
	})

	tenants, err := cfg.TenantsConfig(context.Background())
	require.NoError(t, err)
	require.Len(t, tenants, 3)

	t.Run("scenario 1: tenant pins a dedicated identity schema", func(t *testing.T) {
		t.Parallel()
		strict := findTenant(t, tenants, "tenant-strict")
		assert.Equal(t, "Strict Tenant", strict.Name)
		assert.Equal(t, "employee", strict.SchemaID)
		require.Len(t, strict.RecoveryChannels, 1)
		assert.Equal(t, "email", strict.RecoveryChannels[0].Type)
		assert.True(t, strict.RecoveryChannels[0].Enabled)
	})

	t.Run("scenario 2: tenant restricts recovery channels", func(t *testing.T) {
		t.Parallel()
		emailOnly := findTenant(t, tenants, "tenant-email-only")
		assert.Equal(t, "customer", emailOnly.SchemaID)

		var smsEnabled bool
		for _, ch := range emailOnly.RecoveryChannels {
			if ch.Type == "sms" {
				smsEnabled = ch.Enabled
			}
		}
		assert.False(t, smsEnabled, "sms recovery must be disabled")
	})

	t.Run("scenario 3: tenant without channel policy is unrestricted", func(t *testing.T) {
		t.Parallel()
		open := findTenant(t, tenants, "tenant-open")
		assert.Equal(t, "partner", open.SchemaID)
		assert.Empty(t, open.RecoveryChannels)
	})
}

func TestTenantConfigs_AdapterView(t *testing.T) {
	t.Parallel()

	cfg := newTenantConfig(t, map[string]any{
		config.ViperKeyTenants: []any{
			map[string]any{
				"id":                "acme",
				"schema_id":         "employee",
				"recovery_channels": []any{emailChannel()},
			},
		},
	})

	provider := tenant.NewConfigProvider(cfg)
	view, err := provider.TenantConfigs(context.Background())
	require.NoError(t, err)
	require.Len(t, view, 1)

	got, err := view.FindByID("acme")
	require.NoError(t, err)
	assert.Equal(t, "employee", got.SchemaID)

	_, err = view.FindByID("missing")
	require.Error(t, err)
}

func TestTenantConfigs_EmptyMeansSingleTenant(t *testing.T) {
	t.Parallel()

	cfg := newTenantConfig(t, map[string]any{})
	tenants, err := cfg.TenantsConfig(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tenants)

	// The adapter still returns an empty view, which the tenant layer
	// interprets as single-tenant mode.
	view, err := tenant.NewConfigProvider(cfg).TenantConfigs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, view)
}

func findTenant(t *testing.T, tenants config.Tenants, id string) config.Tenant {
	t.Helper()
	for _, tenant := range tenants {
		if tenant.ID == id {
			return tenant
		}
	}
	t.Fatalf("tenant %q not configured", id)
	return config.Tenant{}
}
