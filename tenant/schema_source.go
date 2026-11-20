// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"encoding/json"
	"io"

	"github.com/pkg/errors"

	"github.com/ory/jsonschema/v3"
	"github.com/ory/kratos/schema"
)

// schemaListProvider is the minimum registry surface the document
// loader needs. Both the default schema provider and the test stubs
// satisfy it.
type schemaListProvider interface {
	schema.IdentitySchemaProvider
}

// ConfigSchemaSource resolves raw identity schema documents for the
// inheritance resolver. It reuses the configured identity schema list
// and the same URL loading machinery as the validator, so operator
// schemes (file://, base64://, https://) behave identically.
type ConfigSchemaSource struct {
	provider schemaListProvider
}

// NewConfigSchemaSource builds a document loader.
func NewConfigSchemaSource(provider schemaListProvider) *ConfigSchemaSource {
	return &ConfigSchemaSource{provider: provider}
}

// RawSchemaDocument implements SchemaSource.
func (s *ConfigSchemaSource) RawSchemaDocument(schemaID string) (map[string]any, error) {
	ctx := context.Background()

	schemas, err := s.provider.IdentityTraitsSchemas(ctx)
	if err != nil {
		return nil, err
	}
	found, err := schemas.GetByID(schemaID)
	if err != nil {
		return nil, ErrSchemaInheritance(schemaID, schemaID, err)
	}

	resource, err := jsonschema.LoadURL(ctx, found.URL.String())
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() { _ = resource.Close() }()

	data, err := io.ReadAll(io.LimitReader(resource, schema.MaxSchemaBodyBytes+1))
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if len(data) > schema.MaxSchemaBodyBytes {
		return nil, errors.Errorf("identity schema rejected: body exceeds %d bytes", schema.MaxSchemaBodyBytes)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.WithStack(err)
	}
	return doc, nil
}
