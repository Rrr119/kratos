// Copyright © 2025 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/ory/kratos/session"
)

// SessionCompatibility keeps tenant information on sessions without
// changing the sessions table or breaking sessions issued before
// multi-tenancy was enabled.
//
// The tenant id is carried in the session's existing JSON surface:
//
//   - On the wire it is exposed as the top-level `tenant_id` claim.
//   - In storage it rides along in the identity's public metadata, which
//     is already persisted and marshaled, so no database migration is
//     required and old session rows decode unchanged.
//
// Sessions that predate this feature simply have no tenant id and
// resolve to DefaultID, which keeps every existing deployment valid.
type SessionCompatibility struct{}

// NewSessionCompatibility builds the helper.
func NewSessionCompatibility() *SessionCompatibility {
	return &SessionCompatibility{}
}

// SetTenant stamps tenantID onto a session. The identity's public
// metadata is the persistence carrier; the same value is also returned
// by TenantOf so the JSON API can expose it without a schema change.
func (SessionCompatibility) SetTenant(s *session.Session, tenantID string) error {
	if s == nil {
		return nil
	}
	tenantID = normalize(tenantID)

	metadata := []byte(s.Identity.MetadataPublic)
	if len(metadata) == 0 || !gjson.ValidBytes(metadata) {
		metadata = []byte(`{}`)
	}
	updated, err := sjson.SetBytes(metadata, SessionClaim, tenantID)
	if err != nil {
		return err
	}
	s.Identity.MetadataPublic = updated
	return nil
}

// TenantOf returns the tenant id associated with a session. Sessions
// issued before multi-tenancy, and sessions without an identity, resolve
// to DefaultID rather than an error so existing cookies keep working.
func (SessionCompatibility) TenantOf(s *session.Session) string {
	if s == nil || s.Identity == nil {
		return DefaultID
	}
	metadata := s.Identity.MetadataPublic
	if len(metadata) == 0 {
		return DefaultID
	}
	if id := gjson.GetBytes(metadata, SessionClaim).String(); id != "" {
		return id
	}
	return DefaultID
}

// MarshalSession renders a session as JSON with the top-level
// `tenant_id` claim added. It wraps the standard session JSON instead of
// re-implementing it, so every existing field and omitempty rule is
// preserved exactly.
func (c SessionCompatibility) MarshalSession(s *session.Session) ([]byte, error) {
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	tenantID := c.TenantOf(s)
	return sjson.SetBytes(encoded, SessionClaim, tenantID)
}

// UnmarshalSession decodes a session payload produced by MarshalSession.
// Payloads without the claim (legacy sessions) decode normally and the
// tenant resolves to DefaultID on the next TenantOf call.
func (c SessionCompatibility) UnmarshalSession(raw []byte, s *session.Session) error {
	if err := json.Unmarshal(raw, s); err != nil {
		return err
	}
	tenantID := gjson.GetBytes(raw, SessionClaim).String()
	if tenantID == "" {
		return nil
	}
	return c.SetTenant(s, tenantID)
}

// SessionClaims returns the JWT claims that must be added when a session
// is tokenized. Returning a separate map keeps the tokenizer templates
// untouched while still surfacing the tenant to API consumers.
func (c SessionCompatibility) SessionClaims(s *session.Session) map[string]any {
	return map[string]any{SessionClaim: c.TenantOf(s)}
}
