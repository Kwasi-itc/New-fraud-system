// Package access defines the verified identity passed from authentication to
// application services. A caller-supplied actor or tenant header is not identity.
package access

import (
	"context"
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
	Kind     Kind
	Subject  string
	TenantID uuid.UUID
	Admin    bool
	// Service principals can operate only within explicitly configured tenants.
	ServiceTenants []uuid.UUID
}
type contextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	p.ServiceTenants = append([]uuid.UUID(nil), p.ServiceTenants...)
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
	if p.Kind == User && p.TenantID == tenantID {
		return p, nil
	}
	if p.Kind == Service {
		for _, id := range p.ServiceTenants {
			if id == tenantID {
				return p, nil
			}
		}
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
	if p.Kind != User || !p.Admin {
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
