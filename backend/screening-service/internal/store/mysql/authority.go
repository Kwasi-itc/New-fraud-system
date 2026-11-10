package mysql

import (
	"context"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type principalKey struct{}
type tenantKey struct{}
type workKey struct{}
type workFence struct{ Tenant, Kind, ID, Token string }

func WithWork(ctx context.Context, tenant, kind, key, token string) context.Context {
	return context.WithValue(ctx, workKey{}, workFence{tenant, kind, key, token})
}

func WithPrincipal(ctx context.Context, p identity.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func WithTenant(ctx context.Context, t uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantKey{}, t)
}
func (s *Store) CheckIdentity(ctx context.Context, p identity.Principal, lock bool) error {
	if p.SessionID == uuid.Nil || p.TenantID == uuid.Nil {
		return identity.Forbidden()
	}
	var mask int64
	e := s.q.QueryRowContext(ctx, "SELECT "+s.schema+".identity_session_roles_v1(?,?,?,?,?)", id(p.Subject), mysqlstore.UUID(p.TenantID), mysqlstore.UUID(p.SessionID), p.AuthzVersion, lock).Scan(&mask)
	if e != nil {
		return &identity.Error{Code: 503, Message: "Identity verification is temporarily unavailable."}
	}
	if mask&6 == 0 || (p.Admin && mask&2 == 0) {
		return identity.Forbidden()
	}
	return nil
}
func (s *Store) authority(ctx context.Context, lock bool) error {
	if fence, ok := ctx.Value(workKey{}).(workFence); ok {
		if e := s.LockWork(ctx, fence.Tenant, fence.Kind, fence.ID, fence.Token); e != nil {
			return e
		}
	}
	if p, ok := ctx.Value(principalKey{}).(identity.Principal); ok {
		return s.CheckIdentity(ctx, p, lock)
	}
	if t, ok := ctx.Value(tenantKey{}).(uuid.UUID); ok {
		var active bool
		q := "SELECT active FROM " + s.schema + ".identity_tenants WHERE id=?"
		if lock {
			q += " FOR SHARE"
		}
		if e := s.q.QueryRowContext(ctx, q, mysqlstore.UUID(t)).Scan(&active); e != nil {
			return normalize(e)
		}
		if !active {
			return identity.Forbidden()
		}
	}
	return nil
}
