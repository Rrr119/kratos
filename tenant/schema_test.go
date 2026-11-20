// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/kratos/tenant"
)

// memorySchemaSource serves schema documents from an in-memory map so
// the inheritance tests do not need files or HTTP.
type memorySchemaSource map[string]map[string]any

func (m memorySchemaSource) RawSchemaDocument(id string) (map[string]any, error) {
	doc, ok := m[id]
	if !ok {
		return nil, tenant.ErrSchemaInheritance(id, id, assert.AnError)
	}
	return doc, nil
}

func mustDoc(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))
	return doc
}

func TestSchemaResolver_InheritsProperties(t *testing.T) {
	t.Parallel()

	source := memorySchemaSource{
		"base": mustDoc(t, `{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type": "object",
			"properties": {
				"traits": {
					"type": "object",
					"properties": {
						"email": {"type": "string", "format": "email"},
						"name": {"type": "string"}
					},
					"required": ["email"]
				}
			}
		}`),
		"child": mustDoc(t, `{
			"extends": "base",
			"properties": {
				"traits": {
					"type": "object",
					"properties": {
						"tenant": {"type": "string"},
						"name": {"type": "string", "minLength": 3}
					},
					"required": ["tenant"]
				}
			}
		}`),
	}

	resolver := tenant.NewSchemaResolver(source)
	merged, err := resolver.Resolve("child")
	require.NoError(t, err)

	// The non-standard keyword must be stripped before compilation.
	_, hasExtends := merged["extends"]
	assert.False(t, hasExtends)

	traits := merged["properties"].(map[string]any)["traits"].(map[string]any)
	props := traits["properties"].(map[string]any)

	// Parent field retained, child field added.
	assert.Contains(t, props, "email")
	assert.Contains(t, props, "tenant")

	// Child override wins for the shared field.
	name := props["name"].(map[string]any)
	assert.Equal(t, float64(3), name["minLength"])

	// required is the union across the chain, parent requirements survive.
	required := traits["required"].([]any)
	assert.ElementsMatch(t, []any{"email", "tenant"}, required)
}

func TestSchemaResolver_MultiLevelInheritance(t *testing.T) {
	t.Parallel()

	source := memorySchemaSource{
		"root": mustDoc(t, `{"type":"object","properties":{"traits":{"type":"object","properties":{"a":{"type":"string"}}}}}`),
		"mid":  mustDoc(t, `{"extends":"root","properties":{"traits":{"type":"object","properties":{"b":{"type":"string"}}}}}`),
		"leaf": mustDoc(t, `{"extends":"mid","properties":{"traits":{"type":"object","properties":{"c":{"type":"string"}}}}}`),
	}

	resolver := tenant.NewSchemaResolver(source)
	merged, err := resolver.Resolve("leaf")
	require.NoError(t, err)

	props := merged["properties"].(map[string]any)["traits"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, props, "a")
	assert.Contains(t, props, "b")
	assert.Contains(t, props, "c")
}

func TestSchemaResolver_RejectsCycle(t *testing.T) {
	t.Parallel()

	source := memorySchemaSource{
		"a": mustDoc(t, `{"extends":"b"}`),
		"b": mustDoc(t, `{"extends":"a"}`),
	}

	resolver := tenant.NewSchemaResolver(source)
	_, err := resolver.Resolve("a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestSchemaResolver_UnknownParent(t *testing.T) {
	t.Parallel()

	source := memorySchemaSource{
		"child": mustDoc(t, `{"extends":"missing"}`),
	}

	resolver := tenant.NewSchemaResolver(source)
	_, err := resolver.Resolve("child")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to resolve identity schema inheritance")
}
