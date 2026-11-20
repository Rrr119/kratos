// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"github.com/ory/herodot"
)

// DefaultID is the tenant identifier used when a request or session does
// not carry an explicit tenant. It keeps existing single-tenant
// deployments and sessions issued before multi-tenancy was enabled
// working unchanged.
const DefaultID = "default"

// HeaderName is the HTTP header (and query parameter) that carries the
// tenant identifier on self-service requests.
const HeaderName = "X-Kratos-Tenant"

// SessionClaim is the key under which the tenant id is stored inside a
// session's JSON payload and JWT claims.
const SessionClaim = "tenant_id"

// ErrUnknownTenant is returned when a request references a tenant that
// has no configuration entry.
func ErrUnknownTenant(id string) error {
	return herodot.ErrBadRequest().
		WithID("tenant_unknown").
		WithError("the requested tenant does not exist").
		WithReasonf(`Tenant %q is not configured.`, id)
}

// ErrChannelNotAllowed is returned when a recovery flow tries to use a
// delivery channel that the tenant configuration disables.
func ErrChannelNotAllowed(tenantID, channel string) error {
	return herodot.ErrForbidden().
		WithID("tenant_recovery_channel_not_allowed").
		WithError("the recovery channel is not allowed for this tenant").
		WithReasonf(`Recovery channel %q is not enabled for tenant %q.`, channel, tenantID)
}

// ErrSchemaCycle is returned when the `extends` graph of tenant identity
// schemas contains a cycle.
func ErrSchemaCycle(id string) error {
	return herodot.ErrMisconfiguration().
		WithID("tenant_schema_cycle").
		WithError("the identity schema inheritance chain contains a cycle").
		WithReasonf(`Schema %q participates in an inheritance cycle.`, id)
}

// ErrSchemaInheritance is returned when a schema extends a schema that
// cannot be resolved.
func ErrSchemaInheritance(id, parent string, cause error) error {
	return herodot.ErrMisconfiguration().
		WithID("tenant_schema_inheritance").
		WithError("unable to resolve identity schema inheritance").
		WithReasonf(`Schema %q extends unknown schema %q.`, id, parent).
		WithDebug(cause.Error())
}
