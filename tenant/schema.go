// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"encoding/json"

	"github.com/pkg/errors"
)

// ExtendsKey is the schema-document keyword that names the parent schema
// id this document inherits from. The value is the `id` of another entry
// in `identity.schemas`, not a JSON pointer or URL, so inheritance stays
// resolvable without fetching external resources.
const ExtendsKey = "extends"

// SchemaSource provides the raw, parsed identity schema document for a
// configured schema id. The driver registry implements this on top of
// the existing schema loader.
type SchemaSource interface {
	// RawSchemaDocument returns the parsed JSON document registered under
	// the given identity-schema id.
	RawSchemaDocument(schemaID string) (map[string]any, error)
}

// SchemaResolver materializes the effective identity schema for a tenant
// by following the `extends` chain and deep-merging every document.
type SchemaResolver struct {
	source SchemaSource
}

// NewSchemaResolver builds a resolver backed by source.
func NewSchemaResolver(source SchemaSource) *SchemaResolver {
	return &SchemaResolver{source: source}
}

// Resolve returns the merged schema document that applies to schemaID.
//
// Inheritance semantics:
//
//   - A document may declare `extends: "<parent-schema-id>"`.
//   - The chain is resolved root-first, so descendants override ancestors.
//   - Objects are merged key by key; scalar values and arrays in the
//     child replace the parent value, except for `required`, which is the
//     union of the chain (a tenant can add required fields but cannot
//     relax a parent's requirement).
//   - Nested `properties` objects merge recursively, allowing a tenant to
//     add fields without re-declaring the parent's trait model.
//   - Cycles in the `extends` graph fail with ErrSchemaCycle.
//
// The returned document never contains the non-standard `extends` key.
func (r *SchemaResolver) Resolve(schemaID string) (map[string]any, error) {
	return r.resolve(schemaID, map[string]struct{}{})
}

func (r *SchemaResolver) resolve(schemaID string, chain map[string]struct{}) (map[string]any, error) {
	if _, seen := chain[schemaID]; seen {
		return nil, errors.WithStack(ErrSchemaCycle(schemaID))
	}
	chain[schemaID] = struct{}{}
	defer delete(chain, schemaID)

	doc, err := r.source.RawSchemaDocument(schemaID)
	if err != nil {
		return nil, err
	}

	parentID, _ := doc[ExtendsKey].(string)
	if parentID == "" {
		return withoutExtends(doc), nil
	}

	parent, err := r.resolve(parentID, chain)
	if err != nil {
		return nil, err
	}

	child := withoutExtends(doc)
	return mergeSchema(parent, child), nil
}

// ResolveBytes is the convenience wrapper that returns the merged
// document re-encoded as JSON, ready to feed into the schema compiler.
func (r *SchemaResolver) ResolveBytes(schemaID string) (json.RawMessage, error) {
	merged, err := r.Resolve(schemaID)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return encoded, nil
}

func withoutExtends(doc map[string]any) map[string]any {
	if _, ok := doc[ExtendsKey]; !ok {
		return doc
	}
	clone := make(map[string]any, len(doc))
	for k, v := range doc {
		if k == ExtendsKey {
			continue
		}
		clone[k] = v
	}
	return clone
}

// mergeSchema deep-merges child into parent and returns a new document.
// Neither input is mutated: nested maps are cloned on the merge path.
func mergeSchema(parent, child map[string]any) map[string]any {
	merged := make(map[string]any, len(parent)+len(child))
	for k, v := range parent {
		merged[k] = v
	}
	for key, childVal := range child {
		parentVal, ok := merged[key]
		if !ok {
			merged[key] = childVal
			continue
		}
		merged[key] = mergeValue(key, parentVal, childVal)
	}
	return merged
}

func mergeValue(key string, parentVal, childVal any) any {
	switch key {
	case "required":
		return mergeRequired(parentVal, childVal)
	default:
		parentMap, parentIsMap := parentVal.(map[string]any)
		childMap, childIsMap := childVal.(map[string]any)
		if parentIsMap && childIsMap {
			return mergeSchema(parentMap, childMap)
		}
		// Scalars and arrays: child wins. Arrays inside `properties`
		// (e.g. `enum`, `oneOf`) intentionally replace the parent so a
		// tenant can fully redefine a field's constraints.
		return childVal
	}
}

// mergeRequired unions the two `required` arrays while preserving order
// and de-duplicating entries. Parent requirements always win over a
// child that tries to drop them.
func mergeRequired(parentVal, childVal any) []any {
	seen := map[string]struct{}{}
	out := make([]any, 0)
	for _, group := range []any{parentVal, childVal} {
		items, ok := group.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			name, ok := item.(string)
			if !ok {
				out = append(out, item)
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}
