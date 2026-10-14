package platform

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

func TestConfiguredLoginTenant(t *testing.T) {
	s, oldTenant := fixture(t)
	ctx := context.Background()
	user, oldRaw := provision(t, s, oldTenant, "operator", "tenant_admin", "case_manager_admin", "platform_admin")
	target := uuid.New()
	if _, err := s.DB.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Target','target_login_test','active',now(),now())`, target); err != nil {
		t.Fatal(err)
	}
	assertDenied := func(tenant uuid.UUID) {
		t.Helper()
		var before, after int
		if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM identity.sessions WHERE user_id=$1`, user).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if raw, err := s.CreateSessionForTenant(ctx, "https://provider.test", "operator", tenant); !errors.Is(err, ErrDenied) || raw != "" {
			t.Fatal("unauthorized login accepted", raw != "", err)
		}
		if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM identity.sessions WHERE user_id=$1`, user).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatal("failed login persisted a session")
		}
	}
	assertDenied(target) // Existing membership in another tenant cannot authorize this one.
	assertDenied(uuid.New())
	same, err := s.Provision(ctx, target, "https://provider.test", "operator", "Operator", []string{"tenant_admin", "case_manager_admin", "platform_admin"})
	if err != nil || same != user {
		t.Fatal("existing user membership", same, err)
	}
	for _, tenant := range []uuid.UUID{target, oldTenant} {
		raw, err := s.CreateSessionForTenant(ctx, "https://provider.test", "operator", tenant)
		if err != nil {
			t.Fatal(err)
		}
		v, err := s.Resolve(ctx, raw)
		if err != nil || v.TenantID != tenant || !allowed(t, s, v, true) {
			t.Fatal("wrong session tenant or case access", v.TenantID, err)
		}
	}
	if v, err := s.Resolve(ctx, oldRaw); err != nil || v.TenantID != oldTenant {
		t.Fatal("existing session was rewritten", err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE identity.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, target, user); err != nil {
		t.Fatal(err)
	}
	assertDenied(target)
	if _, err := s.DB.Exec(ctx, `UPDATE identity.memberships SET active=true WHERE tenant_id=$1 AND user_id=$2`, target, user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE core.tenants SET status='suspended' WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	assertDenied(target)
}
