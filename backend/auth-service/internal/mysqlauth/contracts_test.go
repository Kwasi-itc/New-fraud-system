package mysqlauth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func restrictedStore(t *testing.T, f fixture, worker bool) *Store {
	t.Helper()
	user := "auth_role_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	password := strings.Repeat("b", 64)
	mustExec(t, f, "CREATE USER '"+user+"'@'%' IDENTIFIED BY '"+password+"'")
	t.Cleanup(func() {
		if _, e := f.db.Exec("DROP USER '" + user + "'@'%'"); e != nil {
			t.Error(e)
		}
	})
	grants, e := RuntimeGrants(f.database, user, worker)
	if e != nil {
		t.Fatal(e)
	}
	for _, grant := range grants {
		mustExec(t, f, grant)
	}
	db, e := mysqlstore.Open(context.Background(), mysqlstore.Config{Address: "mysql:3306", Database: f.database, User: user, Password: password, AllowInsecureTransport: true, MaxOpen: 4, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 3 * time.Second, InterpolateParameters: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s, e := New(db, f.database)
	if e != nil {
		t.Fatal(e)
	}
	s.role = "api"
	if worker {
		s.role = "worker"
	}
	if e = s.Ready(context.Background()); e != nil {
		t.Fatal("restricted readiness", e)
	}
	return s
}

func TestMySQLRestrictedRolesAndLoginRetention(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	api := restrictedStore(t, f, false)
	worker := restrictedStore(t, f, true)
	if _, e := api.Resolve(ctx, f.raw); e != nil {
		t.Fatal(e)
	}
	inv, e := NewInvitations(api, f.issuer, "http://localhost:3000", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	invite := uuid.New()
	if _, e = inv.Create(ctx, f.raw, invite, "Grants@example.test", []string{"platform_viewer"}); e != nil {
		t.Fatal(e)
	}
	wi, e := NewInvitations(worker, f.issuer, "http://localhost:3000", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	if claimed, e := wi.DeliverOne(ctx, mailFunc(func(context.Context, string, string, string) error { return nil })); e != nil || !claimed {
		t.Fatal("restricted delivery", claimed, e)
	}
	for _, query := range []string{"UPDATE " + api.table("identity_tenants") + " SET active=FALSE", "DELETE FROM " + api.table("identity_audit_events"), "UPDATE " + api.table("identity_audit_events") + " SET action='forged'", "CREATE TABLE " + api.table("forbidden_schema") + "(id INT)"} {
		if _, e = api.DB.ExecContext(ctx, query); e == nil {
			t.Fatal("API excessive privilege", query)
		}
	}
	for _, query := range []string{"SELECT digest FROM " + worker.table("identity_sessions"), "SELECT roles FROM " + worker.table("identity_memberships"), "SELECT subject FROM " + worker.table("identity_external_identities"), "UPDATE " + worker.table("identity_invitations") + " SET status='cancelled'", "DELETE FROM " + worker.table("identity_audit_events")} {
		if _, e = worker.DB.ExecContext(ctx, query); e == nil {
			t.Fatal("worker excessive privilege", query)
		}
	}
	for n := 0; n < 110; n++ {
		l, e := f.store.BeginLogin(ctx, f.issuer)
		if e != nil {
			t.Fatal(e)
		}
		mustExec(t, f, "UPDATE "+f.store.table("identity_login_transactions")+" SET expires_at=TIMESTAMPADD(MINUTE,-1,SYSDATE(6)) WHERE digest=?", sessioncredential.Digest(l.State))
	}
	live, e := f.store.BeginLogin(ctx, f.issuer)
	if e != nil {
		t.Fatal(e)
	}
	if count, e := worker.PurgeExpiredLogins(ctx); e != nil || count != 100 {
		t.Fatal("retention cap", count, e)
	}
	if count, e := worker.PurgeExpiredLogins(ctx); e != nil || count != 10 {
		t.Fatal(count, e)
	}
	if _, e = f.store.ConsumeLogin(ctx, live.State, live.Binding, f.issuer); e != nil {
		t.Fatal("retention deleted live login", e)
	}
	// Readiness must notice a removed required mutation grant, not merely ping.
	var account string
	if e = api.DB.QueryRow("SELECT CURRENT_USER()").Scan(&account); e != nil {
		t.Fatal(e)
	}
	user := strings.Split(account, "@")[0]
	mustExec(t, f, "REVOKE UPDATE ON "+f.store.table("identity_memberships")+" FROM '"+user+"'@'%'")
	if e = api.Ready(ctx); e == nil {
		t.Fatal("missing required grant reported ready")
	}
}

func TestMySQLAuthorityExpiresAfterTargetLockWait(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	target, e := f.store.Provision(ctx, f.tenant, f.issuer, "locked-member", "Locked", []string{"platform_viewer"})
	if e != nil {
		t.Fatal(e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_sessions")+" SET idle_until=TIMESTAMPADD(SECOND,2,SYSDATE(6)) WHERE digest=?", sessioncredential.Digest(f.raw))
	lock, e := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback()
	var version int64
	if e = lock.QueryRow("SELECT version FROM "+f.store.table("identity_memberships")+" WHERE tenant_id=? AND user_id=? FOR UPDATE", id(f.tenant), id(target)).Scan(&version); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- f.store.ChangeMember(ctx, f.raw, target, version, []string{"platform_builder"}, true) }()
	deadline := time.Now().Add(5 * time.Second)
	waited := false
	for time.Now().Before(deadline) {
		var n int
		e = f.db.QueryRow(`SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=?`, f.database).Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waited {
		t.Fatal("did not observe the target lock wait")
	}
	time.Sleep(2100 * time.Millisecond)
	if e = lock.Rollback(); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, denied) {
			t.Fatal("expired authority accepted after lock wait", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mutation did not return")
	}
	var roles string
	if e = f.db.QueryRow("SELECT roles FROM "+f.store.table("identity_memberships")+" WHERE tenant_id=? AND user_id=?", id(f.tenant), id(target)).Scan(&roles); e != nil || roles != "platform_viewer" {
		t.Fatal("mutation persisted after authority expiry", roles, e)
	}
}

func TestMySQLBoundedListsAndExactLongIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	subject := strings.Repeat("å界", 204) + "ABCD"
	user, e := f.store.Provision(ctx, f.tenant, f.issuer, subject, "Long identity", []string{"platform_viewer"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.CreateSessionForTenant(ctx, f.issuer, strings.TrimSuffix(subject, "ABCD")+"ABCd", f.tenant); !errors.Is(e, denied) {
		t.Fatal("long identity case collision", e)
	}
	if user == uuid.Nil {
		t.Fatal("empty long identity")
	}
	if _, e = f.store.Provision(ctx, f.tenant, f.issuer, subject+"x", "Too long", []string{"platform_viewer"}); !errors.Is(e, denied) {
		t.Fatal("source byte limit changed", e)
	}
	e = f.store.transaction(ctx, func(tx *sql.Tx) error {
		for n := 0; n < 105; n++ {
			u := uuid.New()
			if _, e := tx.ExecContext(ctx, "INSERT INTO "+f.store.table("identity_users")+"(id,display_name,active) VALUES(?,?,TRUE)", id(u), fmt.Sprintf("Bounded %d", n)); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, "INSERT INTO "+f.store.table("identity_memberships")+"(tenant_id,user_id,roles,active,version) VALUES(?,?,'platform_viewer',TRUE,1)", id(f.tenant), id(u)); e != nil {
				return e
			}
			if e := f.store.audit(ctx, tx, f.tenant, "operator-cli", "fixture.created", u, struct{}{}); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	first, e := f.store.Members(ctx, f.raw, uuid.Nil)
	if e != nil || len(first) != 101 {
		t.Fatal("member sentinel", len(first), e)
	}
	second, e := f.store.Members(ctx, f.raw, first[99].UserID)
	if e != nil || len(second) != 7 {
		t.Fatal("member next page", len(second), e)
	}
	if first[100].UserID != second[0].UserID {
		t.Fatal("cursor dropped sentinel")
	}
	audit, e := f.store.Audit(ctx, f.raw, uuid.Nil)
	if e != nil || len(audit) != 101 {
		t.Fatal("audit sentinel", len(audit), e)
	}
	var extra string
	rows, e := f.db.Query("EXPLAIN SELECT user_id FROM "+f.store.table("identity_memberships")+" WHERE tenant_id=? AND user_id>? ORDER BY user_id LIMIT 101", id(f.tenant), id(uuid.Nil))
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	columns, _ := rows.Columns()
	for rows.Next() {
		values := make([]sql.RawBytes, len(columns))
		args := make([]any, len(columns))
		for n := range values {
			args[n] = &values[n]
		}
		if e = rows.Scan(args...); e != nil {
			t.Fatal(e)
		}
		for n, column := range columns {
			if column == "key" {
				extra = string(values[n])
			}
		}
	}
	if e = rows.Err(); e != nil || extra != "PRIMARY" {
		t.Fatal("bounded list lacks its tenant key plan", extra, e)
	}
}

func TestMySQLInvitationResendRateAndFreshInviter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	invite := uuid.New()
	roles := []string{"platform_viewer"}
	if _, e := f.invites.Create(ctx, f.raw, invite, "Cooldown@example.test", roles); e != nil {
		t.Fatal(e)
	}
	old := invitationToken(t, f, invite)
	if e := f.invites.Change(ctx, f.raw, invite, 1, "resend"); !errors.Is(e, conflict) {
		t.Fatal("resend cooldown", e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_invitation_delivery")+" SET requested_at=TIMESTAMPADD(MINUTE,-2,SYSDATE(6)) WHERE invitation_id=?", id(invite))
	if e := f.invites.Change(ctx, f.raw, invite, 1, "resend"); e != nil {
		t.Fatal(e)
	}
	renewed := invitationToken(t, f, invite)
	if renewed == old {
		t.Fatal("resend reused its credential")
	}
	if _, e := f.invites.Begin(ctx, old); !errors.Is(e, denied) {
		t.Fatal("old credential survived resend", e)
	}
	if _, e := f.invites.Begin(ctx, renewed); e != nil {
		t.Fatal(e)
	}
	if e := f.invites.Change(ctx, f.raw, invite, 1, "cancel"); !errors.Is(e, conflict) {
		t.Fatal("stale invitation version", e)
	}
	if e := f.invites.Change(ctx, f.raw, invite, 2, "cancel"); e != nil {
		t.Fatal(e)
	}
	stale := uuid.New()
	if _, e := f.invites.Create(ctx, f.raw, stale, "Stale@example.test", roles); e != nil {
		t.Fatal(e)
	}
	staleToken := invitationToken(t, f, stale)
	mustExec(t, f, "UPDATE "+f.store.table("identity_memberships")+" SET version=version+1 WHERE tenant_id=? AND user_id=?", id(f.tenant), id(f.user))
	if _, e := f.invites.Accept(ctx, sessioncredential.Digest(staleToken), oidcprovider.Identity{Issuer: f.issuer, Subject: "stale-inviter", Name: "Stale", Email: "Stale@example.test", EmailVerified: true}); !errors.Is(e, denied) {
		t.Fatal("stale inviter authority accepted", e)
	}
	for n := 2; n < 100; n++ {
		if _, e := f.invites.Create(ctx, f.raw, uuid.New(), fmt.Sprintf("Rate-%03d@example.test", n), roles); e != nil {
			t.Fatal(n, e)
		}
	}
	if _, e := f.invites.Create(ctx, f.raw, uuid.New(), "Over-rate@example.test", roles); !errors.Is(e, conflict) {
		t.Fatal("tenant mail amplification bound", e)
	}
}

func TestMySQLInvitationLateAuditRollbackAndKeyReuse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	invite := uuid.New()
	mustExec(t, f, "CREATE TRIGGER "+f.store.schema+".auth_invitation_audit_fault BEFORE INSERT ON "+f.store.table("identity_audit_events")+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated invitation audit fault'")
	if _, e := f.invites.Create(ctx, f.raw, invite, "Rollback@example.test", []string{"platform_viewer"}); e == nil {
		t.Fatal("invitation committed without audit")
	}
	for _, table := range []string{"identity_invitations", "identity_invitation_delivery"} {
		var count int
		column := "id"
		if table == "identity_invitation_delivery" {
			column = "invitation_id"
		}
		if e := f.db.QueryRow("SELECT COUNT(*) FROM "+f.store.table(table)+" WHERE "+column+"=?", id(invite)).Scan(&count); e != nil || count != 0 {
			t.Fatal("partial invitation effect", table, count, e)
		}
	}
	mustExec(t, f, "DROP TRIGGER "+f.store.schema+".auth_invitation_audit_fault")
	if _, e := f.invites.Create(ctx, f.raw, invite, "Rollback@example.test", []string{"platform_viewer"}); e != nil {
		t.Fatal("rolled-back key poisoned", e)
	}
}
