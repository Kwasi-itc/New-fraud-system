// Package auth defines the shared authentication and authorization contract.
package auth

import (
	"context"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type PrincipalType string

const (
	PrincipalUser    PrincipalType = "user"
	PrincipalClient  PrincipalType = "client"
	PrincipalService PrincipalType = "service"
)

type Claims struct {
	jwt.RegisteredClaims
	PrincipalType PrincipalType `json:"principal_type"`
	TenantID      uuid.UUID     `json:"tenant_id"`
	Roles         []string      `json:"roles,omitempty"`
	Capabilities  []string      `json:"capabilities,omitempty"`
	Scopes        []string      `json:"scopes,omitempty"`
	ClientID      string        `json:"client_id,omitempty"`
	ServiceID     string        `json:"service_id,omitempty"`
	SessionID     uuid.UUID     `json:"sid,omitempty"`
	AuthzVersion  int64         `json:"authz_version,omitempty"`
	OrigType      PrincipalType `json:"orig_type,omitempty"`
	OrigID        string        `json:"orig_id,omitempty"`
}
type PlatformClaims = Claims
type Principal struct {
	Type         PrincipalType
	Subject      string
	TenantID     uuid.UUID
	SessionID    uuid.UUID
	AuthzVersion int64
	Roles        []string
	Capabilities map[string]bool
	OrigType     PrincipalType
	OrigID       string
	// Admin is derived exclusively from cases:access-all by the verifier.
	// Domain adapters may use it for inbox access, never to authorize other actions.
	Admin bool
}

func (p Principal) Can(capability string) bool { return p.Capabilities[capability] }
func (p Principal) IsHuman() bool              { return p.Type == PrincipalUser }
func (p Principal) IsClient() bool             { return p.Type == PrincipalClient }
func (p Principal) IsService() bool            { return p.Type == PrincipalService }

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	p.Roles = append([]string(nil), p.Roles...)
	caps := make(map[string]bool, len(p.Capabilities))
	for k, v := range p.Capabilities {
		caps[k] = v
	}
	p.Capabilities = caps
	return context.WithValue(ctx, principalKey{}, p)
}
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok && p.Subject != "" && validPrincipalType(p.Type)
}
func validPrincipalType(t PrincipalType) bool {
	return t == PrincipalUser || t == PrincipalClient || t == PrincipalService
}

// WithTestPrincipal injects a principal into an in-process request context.
// It is never read from HTTP headers and does not bypass token authentication.
func WithTestPrincipal(ctx context.Context, p Principal) context.Context {
	return WithPrincipal(ctx, p)
}
