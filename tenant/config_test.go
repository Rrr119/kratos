// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/tenant"
)

func TestConfigs_FindByID(t *testing.T) {
	t.Parallel()

	configs := tenant.Configs{
		{ID: tenant.DefaultID, SchemaID: "default-schema"},
		{ID: "acme", SchemaID: "acme-schema"},
	}

	t.Run("explicit id", func(t *testing.T) {
		t.Parallel()
		got, err := configs.FindByID("acme")
		require.NoError(t, err)
		assert.Equal(t, "acme-schema", got.SchemaID)
	})

	t.Run("empty id resolves to default", func(t *testing.T) {
		t.Parallel()
		got, err := configs.FindByID("")
		require.NoError(t, err)
		assert.Equal(t, tenant.DefaultID, got.ID)
	})

	t.Run("unknown id errors", func(t *testing.T) {
		t.Parallel()
		_, err := configs.FindByID("ghost")
		require.Error(t, err)
	})
}

func TestConfigs_RecoveryChannelFor(t *testing.T) {
	t.Parallel()

	configs := tenant.Configs{
		{
			ID: "with-channels",
			RecoveryChannels: []tenant.Channel{
				{ID: "mail", Type: tenant.RecoveryChannelEmail, Enabled: true},
			},
		},
		{ID: "open", RecoveryChannels: nil},
	}

	t.Run("match by type", func(t *testing.T) {
		t.Parallel()
		ch, err := configs.RecoveryChannelFor("with-channels", "email")
		require.NoError(t, err)
		assert.True(t, ch.Enabled)
	})

	t.Run("match by id", func(t *testing.T) {
		t.Parallel()
		ch, err := configs.RecoveryChannelFor("with-channels", "mail")
		require.NoError(t, err)
		assert.Equal(t, "email", ch.Type)
	})

	t.Run("unrestricted tenant synthesizes an enabled channel", func(t *testing.T) {
		t.Parallel()
		ch, err := configs.RecoveryChannelFor("open", "sms")
		require.NoError(t, err)
		assert.True(t, ch.Enabled)
		assert.Equal(t, "sms", ch.Type)
	})

	t.Run("unknown tenant errors", func(t *testing.T) {
		t.Parallel()
		_, err := configs.RecoveryChannelFor("ghost", "email")
		require.Error(t, err)
	})
}

func TestConfigs_AllowedRecoveryChannels(t *testing.T) {
	t.Parallel()

	configs := tenant.Configs{
		{
			ID: "mixed",
			RecoveryChannels: []tenant.Channel{
				{ID: "mail", Type: "email", Enabled: true},
				{ID: "text", Type: "sms", Enabled: false},
			},
		},
	}

	allowed, err := configs.AllowedRecoveryChannels("mixed")
	require.NoError(t, err)
	require.Len(t, allowed, 1)
	assert.Equal(t, "email", allowed[0].Type)

	_, err = configs.AllowedRecoveryChannels("ghost")
	require.Error(t, err)
}
