// Package access defines the verified identity passed from authentication to
// application services. A caller-supplied actor or tenant header is not identity.
package access

import (
	"context"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"strings"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type Kind string

const (
	User    Kind = "user"
	Service Kind = "service"
)

type Principal struct {
	Kind         Kind
	Subject      string
	TenantID     uuid.UUID
	Admin        bool
	SessionID    uuid.UUID
	Capabilities map[string]bool
	OrigType     auth.PrincipalType
	OrigID       string
	AuthzVersion int64
}
type contextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok && strings.TrimSpace(p.Subject) != "" && (p.Kind == User || p.Kind == Service)
}
func Tenant(ctx context.Context, tenantID uuid.UUID) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok || tenantID == uuid.Nil {
		return Principal{}, casepkg.ErrForbidden
	}
	if (p.Kind == User || p.Kind == Service) && p.TenantID == tenantID {
		return p, nil
	}

	return Principal{}, casepkg.ErrForbidden
}
func Actor(ctx context.Context) *string {
	p, ok := FromContext(ctx)
	if !ok || p.Kind != User {
		return nil
	}
	return &p.Subject
}
func Administrator(ctx context.Context, tenantID uuid.UUID) error {
	p, err := Tenant(ctx, tenantID)
	if err != nil {
		return err
	}
	if p.Kind != User || !p.Capabilities["inboxes:manage"] {
		return casepkg.ErrForbidden
	}
	return nil
}
func Integration(ctx context.Context, tenantID uuid.UUID) error {
	p, err := Tenant(ctx, tenantID)
	if err != nil {
		return err
	}
	if p.Kind != Service {
		return casepkg.ErrForbidden
	}
	return nil
}

func FromPlatform(p auth.Principal) Principal {
	return Principal{Kind: Kind(p.Type), Subject: p.Subject, TenantID: p.TenantID, Admin: p.Can("cases:access-all"), SessionID: p.SessionID, AuthzVersion: p.AuthzVersion, Capabilities: p.Capabilities, OrigType: p.OrigType, OrigID: p.OrigID}
}
func (p Principal) Platform() auth.Principal {
	return auth.Principal{Type: auth.PrincipalType(p.Kind), Subject: p.Subject, TenantID: p.TenantID, Admin: p.Admin, SessionID: p.SessionID, AuthzVersion: p.AuthzVersion, Capabilities: p.Capabilities, OrigType: p.OrigType, OrigID: p.OrigID}
}
