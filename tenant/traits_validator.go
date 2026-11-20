// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/pkg/errors"
	"github.com/tidwall/sjson"

	"github.com/ory/herodot"
	"github.com/ory/jsonschema/v3"
	"github.com/ory/kratos/schema"
)

// inMemoryURL is the synthetic resource URL under which a merged tenant
// schema is registered in the compiler. It never hits the network
// because the resource is added explicitly via AddResource.
const inMemoryURL = "base64://tenant-merged-schema"

// TraitsValidator validates identity traits against a tenant's merged
// (post-inheritance) schema document. It compiles the merged document
// in-memory, so child overrides and unioned `required` fields are
// enforced without writing any temporary files.
type TraitsValidator struct {
	resolver *SchemaResolver
}

// NewTraitsValidator builds a validator over the inheritance resolver.
func NewTraitsValidator(resolver *SchemaResolver) *TraitsValidator {
	return &TraitsValidator{resolver: resolver}
}

// ValidateTraits validates the given traits object against the effective
// schema of schemaID. traits is wrapped under the `traits` root to match
// the document shape identity.Validator uses, keeping error instance
// pointers consistent.
func (v *TraitsValidator) ValidateTraits(ctx context.Context, schemaID string, traits json.RawMessage) error {
	if len(traits) == 0 {
		traits = []byte(`{}`)
	}

	merged, err := v.resolver.ResolveBytes(schemaID)
	if err != nil {
		return err
	}

	compiler := schema.NewCompiler(false)
	if err := compiler.AddResource(inMemoryURL, bytes.NewReader(merged)); err != nil {
		return errors.WithStack(herodot.ErrMisconfiguration().WithWrap(err))
	}
	compiled, err := compiler.Compile(ctx, inMemoryURL)
	if err != nil {
		return errors.WithStack(herodot.ErrMisconfiguration().
			WithReason("Unable to compile the tenant identity schema.").
			WithWrap(err))
	}

	document, err := sjson.SetRawBytes([]byte(`{}`), "traits", traits)
	if err != nil {
		return errors.WithStack(herodot.ErrBadRequest().WithError(err.Error()))
	}

	decoded, err := jsonschema.DecodeJSON(bytes.NewBuffer(document))
	if err != nil {
		return errors.WithStack(herodot.ErrBadRequest().
			WithReason("Unable to parse the identity traits.").
			WithWrap(err))
	}
	if err := compiled.ValidateInterface(decoded); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
