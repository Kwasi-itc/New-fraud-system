package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/google/uuid"
)

func legacyAccount(t *testing.T, s Store, tenant uuid.UUID, subject string, roles []string) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	raw := secret()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO identity.users(id,display_name) VALUES($1,$2)`, []any{id, subject}},
		{`INSERT INTO identity.external_identities(issuer,subject,user_id) VALUES('https://provider.test',$1,$2)`, []any{subject, id}},
		{`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,$3)`, []any{tenant, id, roles}},
		{`INSERT INTO identity.sessions(id,digest,user_id,tenant_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '8 hours',now()+interval '30 minutes')`, []any{uuid.New(), digest(raw), id, tenant}},
	} {
		if _, err := s.DB.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	return id, raw
}
func accountCapabilities(t *testing.T, s Store, tenant, user uuid.UUID) []string {
	t.Helper()
	var caps []string
	if err := s.DB.QueryRow(context.Background(), `SELECT identity.member_capabilities_v1($1,$2)`, tenant, user).Scan(&caps); err != nil {
		t.Fatal(err)
	}
	slices.Sort(caps)
	return caps
}

func TestSingleRoleMigrationPreservesCapabilitiesAndInvitations(t *testing.T) {
	s, tenant := fixtureVersion(t, 5)
	ctx := context.Background()
	admin, adminRaw := legacyAccount(t, s, tenant, "legacy-admin", []string{"tenant_admin", "case_manager_admin", "platform_admin"})
	analyst, _ := legacyAccount(t, s, tenant, "legacy-analyst", []string{"case_investigator", "platform_viewer"})
	twin, _ := legacyAccount(t, s, tenant, "legacy-twin", []string{"platform_viewer", "case_investigator"})
	other := uuid.New()
	if _, err := s.DB.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Other',$2,'active',now(),now())`, other, "tenant_"+strings.ReplaceAll(other.String(), "-", "")); err != nil {
		t.Fatal(err)
	}
	foreign, _ := legacyAccount(t, s, other, "legacy-foreign", []string{"platform_viewer", "case_investigator"})
	before := map[uuid.UUID][]string{admin: accountCapabilities(t, s, tenant, admin), analyst: accountCapabilities(t, s, tenant, analyst), twin: accountCapabilities(t, s, tenant, twin), foreign: accountCapabilities(t, s, other, foreign)}
	invite, token := uuid.New(), secret()
	if _, err := s.DB.Exec(ctx, `INSERT INTO identity.invitations(id,tenant_id,email,issuer,roles,inviter_id,inviter_version,digest,expires_at) VALUES($1,$2,'new@example.test','https://provider.test',ARRAY['case_investigator','platform_viewer'],$3,1,$4,now()+interval '1 hour')`, invite, tenant, admin, digest(token)); err != nil {
		t.Fatal(err)
	}
	historical := uuid.New()
	if _, err := s.DB.Exec(ctx, `INSERT INTO identity.invitations(id,tenant_id,email,issuer,roles,inviter_id,inviter_version,digest,expires_at,status) VALUES($1,$2,'old@example.test','https://provider.test',ARRAY['deleted_role','old_role'],$3,1,$4,now()-interval '1 day','cancelled')`, historical, tenant, admin, digest(secret())); err != nil {
		t.Fatal(err)
	}
	mailer, err := NewInvitations(s, "https://provider.test", "http://localhost:3000", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := mailer.seal(invite, "new@example.test", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO identity.invitation_delivery(invitation_id,generation,payload,state) VALUES($1,1,$2,'queued')`, invite, payload); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var selected []string
	var version int64
	for user, caps := range before {
		owner := tenant
		if user == foreign {
			owner = other
		}
		after := accountCapabilities(t, s, owner, user)
		if !slices.Equal(caps, after) {
			t.Fatalf("capabilities changed for %s", user)
		}
		if err := s.DB.QueryRow(ctx, `SELECT roles,version FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2`, owner, user).Scan(&selected, &version); err != nil {
			t.Fatal(err)
		}
		if len(selected) != 1 || version != 1 {
			t.Fatal("role consolidation or version missing", selected, version)
		}
		if user == admin && selected[0] != "tenant_admin" {
			t.Fatal("dominant Tenant Admin role lost")
		}
	}
	var analystRole, twinRole, foreignRole string
	for user, target := range map[uuid.UUID]*string{analyst: &analystRole, twin: &twinRole, foreign: &foreignRole} {
		if err := s.DB.QueryRow(ctx, `SELECT roles[1] FROM identity.memberships WHERE user_id=$1`, user).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if analystRole != twinRole || analystRole == foreignRole || !customRoleID.MatchString(analystRole) {
		t.Fatal("bundle reuse or tenant isolation failed")
	}
	var inviterVersion int64
	if err := s.DB.QueryRow(ctx, `SELECT roles,inviter_version FROM identity.invitations WHERE id=$1`, invite).Scan(&selected, &inviterVersion); err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0] != analystRole || inviterVersion != 1 {
		t.Fatal("pending invitation changed permissions or lost issuer authority", selected, inviterVersion)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("repeat migration", err)
	}
	var audits int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM identity.audit_events WHERE actor_id='identity-migration-v6'`).Scan(&audits); err != nil || audits != 5 {
		t.Fatal("non-idempotent audit", audits, err)
	}
	resolved, err := s.Resolve(ctx, adminRaw)
	if err != nil || len(resolved.Roles) != 1 {
		t.Fatal("existing opaque session not preserved", err)
	}
	capture := &captureMail{}
	worked, deliveryErr := mailer.DeliverOne(ctx, capture)
	if deliveryErr != nil || !worked || capture.calls != 1 {
		t.Fatal("pending mail generation invalidated", worked, capture.calls, deliveryErr)
	}
	accepted, err := (&InvitationService{Store: s, Issuer: "https://provider.test"}).Accept(ctx, digest(token), oidcprovider.Identity{Issuer: "https://provider.test", Subject: "new-user", Email: "new@example.test", EmailVerified: true, Name: "New user", AccountName: "new_user"})
	if err != nil {
		t.Fatal("pending invitation cannot be accepted", err)
	}
	profile, err := s.Profile(ctx, accepted)
	if err != nil || profile.RoleID != analystRole || profile.AccountName != "new_user" {
		t.Fatal("accepted invitation profile", profile, err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE identity.memberships SET roles=ARRAY['case_investigator','platform_viewer'] WHERE tenant_id=$1 AND user_id=$2`, tenant, analyst); err == nil {
		t.Fatal("database accepted multiple roles")
	}
	if _, err := s.Provision(ctx, tenant, "https://provider.test", "bad", "Bad", []string{"case_investigator", "platform_viewer"}); !errors.Is(err, ErrDenied) {
		t.Fatal("API accepted multiple roles", err)
	}
	if err := s.DB.QueryRow(ctx, `SELECT roles FROM identity.invitations WHERE id=$1`, historical).Scan(&selected); err != nil || len(selected) != 2 {
		t.Fatal("historical invitation audit record changed", err)
	}
}

func TestSingleRoleMigrationAuditFailureRollsBack(t *testing.T) {
	s, tenant := fixtureVersion(t, 5)
	ctx := context.Background()
	user, _ := legacyAccount(t, s, tenant, "rollback-user", []string{"case_investigator", "platform_viewer"})
	if _, err := s.DB.Exec(ctx, `CREATE FUNCTION identity.fail_consolidation() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='membership.role_consolidated' THEN RAISE EXCEPTION 'audit outage'; END IF; RETURN NEW; END$$; CREATE TRIGGER fail_consolidation BEFORE INSERT ON identity.audit_events FOR EACH ROW EXECUTE FUNCTION identity.fail_consolidation()`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("audit failure ignored")
	}
	var roles []string
	var version int
	if err := s.DB.QueryRow(ctx, `SELECT roles FROM identity.memberships WHERE user_id=$1`, user).Scan(&roles); err != nil || len(roles) != 2 {
		t.Fatal("partial membership migration", roles, err)
	}
	if err := s.DB.QueryRow(ctx, `SELECT max(version) FROM identity.schema_migrations`).Scan(&version); err != nil || version != 5 {
		t.Fatal("partial version migration", version, err)
	}
	if _, err := s.DB.Exec(ctx, `DROP TRIGGER fail_consolidation ON identity.audit_events`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("recovery migration failed", err)
	}
}

func TestOwnProfileConcurrencyRevocationAndProviderMetadata(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	user, _ := provision(t, s, tenant, "profile-user", "case_investigator")
	_, otherRaw := provision(t, s, tenant, "other-profile", "platform_viewer")
	raw, err := s.CreateSessionForIdentity(ctx, oidcprovider.Identity{Issuer: "https://provider.test", Subject: "profile-user", Name: "Provider name", AccountName: "profile_login", Email: "profile@example.test", EmailVerified: true}, tenant)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.Profile(ctx, raw)
	if err != nil || profile.UserID != user || profile.AccountName != "profile_login" || !profile.EmailVerified || profile.Name != "profile-user" || profile.RoleID != "case_investigator" {
		t.Fatal("profile metadata or name ownership", profile, err)
	}
	if err = s.ChangeProfile(ctx, raw, "Updated name", profile.Version); err != nil {
		t.Fatal(err)
	}
	other, err := s.Profile(ctx, otherRaw)
	if err != nil || other.Name != "other-profile" {
		t.Fatal("another account changed", other, err)
	}
	updated, err := s.Profile(ctx, raw)
	if err != nil || updated.Name != "Updated name" || updated.Version != 2 {
		t.Fatal(updated, err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	start := make(chan struct{})
	for _, name := range []string{"First editor", "Second editor"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			<-start
			outcomes <- s.ChangeProfile(ctx, raw, name, updated.Version)
		}(name)
	}
	close(start)
	wg.Wait()
	close(outcomes)
	success, conflict := 0, 0
	for outcome := range outcomes {
		if outcome == nil {
			success++
		} else if errors.Is(outcome, ErrConflict) {
			conflict++
		} else {
			t.Fatal(outcome)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent edits lost update", success, conflict)
	}
	current, err := s.Profile(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeProfile(ctx, raw, "After logout", current.Version); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked session changed profile", err)
	}
	if _, err = s.Profile(ctx, raw); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked session read profile", err)
	}
}

func TestProfileAuditRollbackAndValidation(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, raw := provision(t, s, tenant, "profile-rollback", "platform_viewer")
	for _, name := range []string{" ", strings.Repeat("x", 201), "Bad\nname"} {
		if err := s.ChangeProfile(ctx, raw, name, 1); err == nil {
			t.Fatal("invalid profile name accepted")
		}
	}
	if _, err := s.DB.Exec(ctx, `CREATE FUNCTION identity.fail_profile_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.action='profile.updated' THEN RAISE EXCEPTION 'audit outage'; END IF; RETURN NEW; END$$; CREATE TRIGGER fail_profile_audit BEFORE INSERT ON identity.audit_events FOR EACH ROW EXECUTE FUNCTION identity.fail_profile_audit()`); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeProfile(ctx, raw, "Should roll back", 1); err == nil {
		t.Fatal("audit failure ignored")
	}
	profile, err := s.Profile(ctx, raw)
	if err != nil || profile.Version != 1 || profile.Name != "profile-rollback" {
		t.Fatal("profile changed after rollback", profile, err)
	}
}

func TestPendingInvitationsAndAssignmentsRejectMultipleRoles(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, raw := provision(t, s, tenant, "single-admin", "tenant_admin")
	target, _ := provision(t, s, tenant, "single-member", "platform_viewer")
	if err := s.ChangeMember(ctx, raw, target, 1, []string{"case_investigator", "platform_viewer"}, true); !errors.Is(err, ErrDenied) {
		t.Fatal("multiple assigned roles accepted", err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO identity.invitations(id,tenant_id,email,issuer,roles,inviter_id,inviter_version,digest,expires_at) SELECT $1,$2,'bad@example.test','https://provider.test',ARRAY['case_investigator','platform_viewer'],user_id,1,$3,$4 FROM identity.memberships WHERE tenant_id=$2 AND roles=ARRAY['tenant_admin']`, uuid.New(), tenant, digest(secret()), time.Now().Add(time.Hour)); err == nil {
		t.Fatal("multiple invitation roles accepted")
	}
}
