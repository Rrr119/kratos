// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"github.com/ory/kratos/driver/config"
)

// rawTenantProvider is the config surface the adapter reads. It is a
// narrow interface so tests can supply a stub without standing up the
// whole config provider.
type rawTenantProvider interface {
	TenantsConfig(ctx context.Context) (config.Tenants, error)
}

// ConfigProvider adapts the driver config (which holds the native
// configuration structs) to the value-object view the tenant layer uses.
// It lives here rather than in driver/config to keep the import graph
// acyclic: driver/config must not import the tenant package.
type ConfigProvider struct {
	config rawTenantProvider
}

// NewConfigProvider builds the adapter.
func NewConfigProvider(config rawTenantProvider) *ConfigProvider {
	return &ConfigProvider{config: config}
}

// TenantConfigs implements Provider.
func (p *ConfigProvider) TenantConfigs(ctx context.Context) (Configs, error) {
	configured, err := p.config.TenantsConfig(ctx)
	if err != nil {
		return nil, err
	}

	out := make(Configs, 0, len(configured))
	for _, t := range configured {
		channels := make([]Channel, 0, len(t.RecoveryChannels))
		for _, ch := range t.RecoveryChannels {
			channels = append(channels, Channel{
				ID:      ch.ID,
				Type:    ch.Type,
				Enabled: ch.Enabled,
			})
		}
		out = append(out, Config{
			ID:               t.ID,
			Name:             t.Name,
			SchemaID:         t.SchemaID,
			RecoveryChannels: channels,
		})
	}
	return out, nil
}
