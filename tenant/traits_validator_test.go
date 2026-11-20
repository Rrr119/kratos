// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/tenant"
)

func TestTraitsValidator_EnforcesInheritedSchema(t *testing.T) {
	t.Parallel()

	source := memorySchemaSource{
		"base": mustDoc(t, `{
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
		}`),
		"strict": mustDoc(t, `{
			"extends": "base",
			"properties": {
				"traits": {
					"type": "object",
					"properties": {
						"org": {"type": "string", "minLength": 2}
					},
					"required": ["org"]
				}
			}
		}`),
	}

	validator := tenant.NewTraitsValidator(tenant.NewSchemaResolver(source))
	ctx := context.Background()

	t.Run("rejects missing inherited and child required fields", func(t *testing.T) {
		err := validator.ValidateTraits(ctx, "strict", []byte(`{}`))
		require.Error(t, err)
	})

	t.Run("rejects child field constraint violation", func(t *testing.T) {
		err := validator.ValidateTraits(ctx, "strict",
			[]byte(`{"email":"a@b.com","org":"x"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "minLength")
	})

	t.Run("accepts document satisfying the merged schema", func(t *testing.T) {
		err := validator.ValidateTraits(ctx, "strict",
			[]byte(`{"email":"a@b.com","org":"acme"}`))
		require.NoError(t, err)
	})

	t.Run("rejects wrong type from parent schema", func(t *testing.T) {
		err := validator.ValidateTraits(ctx, "strict",
			[]byte(`{"email":123,"org":"acme"}`))
		require.Error(t, err)
	})
}

func TestTraitsValidator_SchemaWithoutInheritance(t *testing.T) {
	t.Parallel()

	source := memorySchemaSource{
		"plain": mustDoc(t, `{
			"type": "object",
			"properties": {
				"traits": {
					"type": "object",
					"properties": {"username": {"type": "string"}}
				}
			}
		}`),
	}

	validator := tenant.NewTraitsValidator(tenant.NewSchemaResolver(source))
	require.NoError(t, validator.ValidateTraits(context.Background(), "plain",
		[]byte(`{"username":"alice"}`)))
}
