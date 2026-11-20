// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package tenant implements multi-tenant identity schema validation,
// tenant-scoped recovery channels, and session compatibility helpers for
// Ory Kratos.
//
// A tenant configuration references an identity schema that may inherit
// from another schema through the `extends` key. Child schema documents
// are deep-merged on top of their parents, so each tenant only needs to
// declare the fields it wants to add or tighten. The merged document is
// then validated with the regular identity-schema machinery.
package tenant

import (
	"context"
)

// Channel is the tenant-package view of a configured recovery channel.
// The concrete configuration struct lives in driver/config to avoid an
// import cycle; accessors return these value objects.
type Channel struct {
	ID      string
	Type    string
	Enabled bool
}

// Config is the tenant-package view of one tenant configuration.
type Config struct {
	ID               string
	Name             string
	SchemaID         string
	RecoveryChannels []Channel
}

// Configs is the ordered set of tenant configurations.
type Configs []Config

// Provider is implemented by the driver config; it returns the tenant
// configurations applicable to the request context.
type Provider interface {
	TenantConfigs(ctx context.Context) (Configs, error)
}

// FindByID returns the tenant configuration for id. The empty id
// resolves to the default tenant when one is configured.
func (c Configs) FindByID(id string) (*Config, error) {
	if id == "" {
		id = DefaultID
	}
	for i := range c {
		if c[i].ID == id {
			return &c[i], nil
		}
	}
	return nil, ErrUnknownTenant(id)
}

// RecoveryChannelFor reports whether the tenant permits recovery over
// the given channel type. Tenants without an explicit channel list allow
// every channel, which preserves the pre-multi-tenant behavior.
func (c Configs) RecoveryChannelFor(tenantID, channelType string) (Channel, error) {
	t, err := c.FindByID(tenantID)
	if err != nil {
		return Channel{}, err
	}
	if len(t.RecoveryChannels) == 0 {
		return Channel{ID: channelType, Type: channelType, Enabled: true}, nil
	}
	for _, ch := range t.RecoveryChannels {
		if ch.Enabled && (ch.Type == channelType || ch.ID == channelType) {
			return ch, nil
		}
	}
	return Channel{}, ErrChannelNotAllowed(tenantID, channelType)
}

// AllowedRecoveryChannels returns the enabled channels for the tenant.
// A nil result means "no restriction" rather than "no channels".
func (c Configs) AllowedRecoveryChannels(tenantID string) ([]Channel, error) {
	t, err := c.FindByID(tenantID)
	if err != nil {
		return nil, err
	}
	if len(t.RecoveryChannels) == 0 {
		return nil, nil
	}
	allowed := make([]Channel, 0, len(t.RecoveryChannels))
	for _, ch := range t.RecoveryChannels {
		if ch.Enabled {
			allowed = append(allowed, ch)
		}
	}
	return allowed, nil
}
