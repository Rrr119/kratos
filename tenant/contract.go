// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"github.com/ory/kratos/identity"
	"github.com/ory/kratos/schema"
)

// Registry is the contract a driver registry must satisfy to back the
// tenant-aware identity validator. It is declared here so the tenant
// package owns the interface while concrete registries implement it
// without an import back into the tenant package's wiring.
type Registry interface {
	Provider
	schema.IdentitySchemaProvider
	IdentityValidator() *identity.Validator
}

// Compile-time guarantee that IdentityValidationDependencies stays
// assignment-compatible with the registry contract.
var _ Registry = (IdentityValidationDependencies)(nil)
