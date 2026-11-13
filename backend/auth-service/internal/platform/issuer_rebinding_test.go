package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestIssuerRebindingPreservesAccessAndIsRetrySafe(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, oldSession := provision(t, s, tenant, "operator", "tenant_admin", "case_manager_admin", "platform_admin")
	before, err := s.Resolve(ctx, oldSession)
	if err != nil {
		t.Fatal(err)
	}
	const next = "http://localhost:8088/identity/realms/fraud"
	if err = s.RebindIssuer(ctx, user, "operator", "https://provider.test", next); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, oldSession); !errors.Is(err, ErrDenied) {
		t.Fatal("old session was not revoked", err)
	}
	if _, err = s.CreateSession(ctx, "https://provider.test", "operator"); !errors.Is(err, ErrDenied) {
		t.Fatal("previous issuer still accepted", err)
	}
	raw, err := s.CreateSessionForTenant(ctx, next, "operator", tenant)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Resolve(ctx, raw)
	if err != nil || after.UserID != before.UserID || after.TenantID != before.TenantID || after.Version != before.Version || !reflect.DeepEqual(after.Roles, before.Roles) {
		t.Fatal("repair changed platform identity or access", err)
	}
	if _, err = s.CreateSessionForTenant(ctx, next, "operator", uuid.New()); !errors.Is(err, ErrDenied) {
		t.Fatal("repair granted another tenant", err)
	}
	if err = s.RebindIssuer(ctx, user, "operator", "https://provider.test", next); err != nil {
		t.Fatal("identical retry failed", err)
	}
	if _, err = s.Resolve(ctx, raw); err != nil {
		t.Fatal("retry revoked a new session", err)
	}
	var events int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM identity.audit_events WHERE tenant_id=$1 AND target_id=$2 AND action='external_identity.issuer_rebound'`, tenant, user).Scan(&events); err != nil || events != 1 {
		t.Fatal("repair audit was missing or duplicated", events, err)
	}
}

func TestIssuerRebindingRejectsDifferentUserAndUnknownSubject(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, raw := provision(t, s, tenant, "operator", "platform_viewer")
	other, err := s.Provision(ctx, tenant, "https://replacement.test", "operator", "Different identity", []string{"platform_viewer"})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []uuid.UUID{user, other} {
		if err = s.RebindIssuer(ctx, expected, "operator", "https://provider.test", "https://replacement.test"); !errors.Is(err, ErrConflict) {
			t.Fatal("conflicting user identity was overwritten", err)
		}
	}
	if err = s.RebindIssuer(ctx, user, "unknown", "https://provider.test", "https://replacement.test"); !errors.Is(err, ErrDenied) {
		t.Fatal("unknown identity accepted", err)
	}
	if _, err = s.Resolve(ctx, raw); err != nil {
		t.Fatal("rejected repair changed session", err)
	}
}

func TestIssuerRebindingAuditFailureRollsBack(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, raw := provision(t, s, tenant, "operator", "platform_viewer")
	if _, err := s.DB.Exec(ctx, `CREATE FUNCTION identity.reject_issuer_audit() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN IF NEW.action='external_identity.issuer_rebound' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$;
	CREATE TRIGGER reject_issuer_audit BEFORE INSERT ON identity.audit_events FOR EACH ROW EXECUTE FUNCTION identity.reject_issuer_audit()`); err != nil {
		t.Fatal(err)
	}
	if err := s.RebindIssuer(ctx, user, "operator", "https://provider.test", "https://replacement.test"); err == nil {
		t.Fatal("repair succeeded without audit")
	}
	if _, err := s.Resolve(ctx, raw); err != nil {
		t.Fatal("session revocation escaped rollback", err)
	}
	if _, err := s.CreateSession(ctx, "https://provider.test", "operator"); err != nil {
		t.Fatal("original binding escaped rollback", err)
	}
	if _, err := s.CreateSession(ctx, "https://replacement.test", "operator"); !errors.Is(err, ErrDenied) {
		t.Fatal("new binding escaped rollback", err)
	}
}

func TestConcurrentIssuerRebindingHasOneAudit(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, _ := provision(t, s, tenant, "operator", "platform_viewer")
	done := make(chan error, 2)
	for range 2 {
		go func() {
			done <- s.RebindIssuer(ctx, user, "operator", "https://provider.test", "https://replacement.test")
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM identity.audit_events WHERE action='external_identity.issuer_rebound'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("competing repairs did not serialize", events, err)
	}
}

func TestIssuerRebindingRequiresSafeExplicitIssuers(t *testing.T) {
	for _, raw := range []string{"", "https://", " http://localhost:8088/identity", "http://provider.example/realm", "https://user:password@provider.example/realm", "https://provider.example/realm?token=value", "https://provider.example/realm#fragment"} {
		if validRebindingIssuer(raw) {
			t.Fatal("unsafe issuer accepted")
		}
	}
}
