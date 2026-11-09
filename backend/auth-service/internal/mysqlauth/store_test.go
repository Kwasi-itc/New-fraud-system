package mysqlauth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/sessioncredential"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type fixture struct {
	store        *Store
	db           *sql.DB
	database     string
	tenant, user uuid.UUID
	raw, issuer  string
	invites      *Invitations
}

func rootDB(t *testing.T, address, database string) *sql.DB {
	t.Helper()
	c := driver.NewConfig()
	c.Net = "tcp"
	c.Addr = address
	c.User = "root"
	c.Passwd = "isolated-auth-test-root"
	c.DBName = database
	c.ParseTime = true
	c.Loc = time.UTC
	c.Timeout = 5 * time.Second
	c.ReadTimeout = 15 * time.Second
	c.WriteTimeout = 15 * time.Second
	c.Params = map[string]string{"time_zone": "'+00:00'", "transaction_isolation": "'READ-COMMITTED'"}
	connector, e := driver.NewConnector(c)
	if e != nil {
		t.Fatal(e)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(8)
	if e = db.Ping(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func newFixture(t *testing.T) fixture {
	t.Helper()
	if os.Getenv("AUTH_MYSQL_TEST_ADDRESS") == "" {
		if os.Getenv("AUTH_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required MySQL fixture missing")
		}
		t.Skip("private native MySQL fixture required")
	}
	if os.Getenv("AUTH_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("AUTH_MYSQL_REFERENCE_ADDRESS") != "mysql-reference:3306" {
		t.Fatal("only the private Compose fixture is accepted")
	}
	ctx := context.Background()
	database := "auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, _ := mysqlstore.Identifier(database)
	targetAdmin, referenceAdmin := rootDB(t, "mysql:3306", ""), rootDB(t, "mysql-reference:3306", "")
	for _, db := range []*sql.DB{targetAdmin, referenceAdmin} {
		var version string
		if e := db.QueryRow("SELECT VERSION()").Scan(&version); e != nil || version != "8.4.8" {
			t.Fatal("pinned native engine required", version, e)
		}
		if _, e := db.Exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if _, e := db.Exec("DROP DATABASE " + schema); e != nil {
				t.Error(e)
			}
		})
		if _, e := db.Exec("CREATE USER IF NOT EXISTS 'auth_fixture_guard'@'localhost' ACCOUNT LOCK"); e != nil {
			t.Fatal(e)
		}
	}
	db, reference := rootDB(t, "mysql:3306", database), rootDB(t, "mysql-reference:3306", database)
	base, e := mysqlidentity.Statements(database, "auth_fixture_guard")
	if e != nil {
		t.Fatal(e)
	}
	app, e := Statements(database)
	if e != nil {
		t.Fatal(e)
	}
	for _, owner := range []struct {
		name  string
		steps []migrations.Step
	}{{"identity", base}, {Owner, app}} {
		plan := migrations.Plan{Database: database, Owner: owner.name, Steps: owner.steps}
		conn, e := reference.Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for n := range plan.Steps {
			step := &plan.Steps[n]
			if _, e = conn.ExecContext(ctx, step.SQL); e != nil {
				t.Fatal(step.ID, e)
			}
			step.After, e = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
			if e != nil {
				t.Fatal(e)
			}
		}
		conn.Close()
		runner, e := migrations.NewForOwner(db, owner.name, plan)
		if e != nil {
			t.Fatal(e)
		}
		if e = runner.Apply(ctx); e != nil {
			t.Fatal(e)
		}
		if e = runner.Apply(ctx); e != nil {
			t.Fatal("migration replay", e)
		}
	}
	grants, e := mysqlidentity.DefinerGrants(database, "auth_fixture_guard")
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range grants {
		if _, e = db.Exec(g); e != nil {
			t.Fatal(e)
		}
	}
	s, e := New(db, database)
	if e != nil {
		t.Fatal(e)
	}
	tenant := uuid.New()
	if e = s.RegisterTenant(ctx, tenant); e != nil {
		t.Fatal(e)
	}
	issuer := "https://auth-provider.test"
	user, e := s.Provision(ctx, tenant, issuer, "administrator", "Administrator", []string{"tenant_admin", "platform_admin"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s.CreateSessionForTenant(ctx, issuer, "administrator", tenant)
	if e != nil {
		t.Fatal(e)
	}
	inv, e := NewInvitations(s, issuer, "http://localhost:3000", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Ready(ctx); e != nil {
		t.Fatal(e)
	}
	return fixture{s, db, database, tenant, user, raw, issuer, inv}
}
func mustExec(t *testing.T, f fixture, query string, args ...any) {
	t.Helper()
	if _, e := f.db.Exec(query, args...); e != nil {
		t.Fatal(e)
	}
}
func invitationToken(t *testing.T, f fixture, invite uuid.UUID) string {
	t.Helper()
	var payload []byte
	if e := f.db.QueryRow("SELECT payload FROM "+f.store.table("identity_invitation_delivery")+" WHERE invitation_id=?", id(invite)).Scan(&payload); e != nil {
		t.Fatal(e)
	}
	p, e := f.invites.Envelope.Open(invite, payload)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(payload), p.Token) || strings.Contains(string(payload), p.Email) {
		t.Fatal("plaintext invitation material persisted")
	}
	return p.Token
}

func TestMySQLLoginSingleUseAndIdentityBindings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l, e := f.store.BeginLogin(ctx, f.issuer)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.ConsumeLogin(ctx, l.State, l.Binding+"x", f.issuer); !errors.Is(e, denied) {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := f.store.ConsumeLogin(ctx, l.State, l.Binding, f.issuer)
			if e == nil {
				if got.Verifier != l.Verifier || got.Nonce != l.Nonce {
					t.Error("changed PKCE binding")
				}
				wins.Add(1)
			} else if !errors.Is(e, denied) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("login consumption winners", wins.Load())
	}
	l, e = f.store.BeginLogin(ctx, f.issuer)
	if e != nil {
		t.Fatal(e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_login_transactions")+" SET expires_at=TIMESTAMPADD(MINUTE,-1,SYSDATE(6)) WHERE digest=?", sessioncredential.Digest(l.State))
	if _, e = f.store.ConsumeLogin(ctx, l.State, l.Binding, f.issuer); !errors.Is(e, denied) {
		t.Fatal("expired login", e)
	}
	if _, e = f.store.CreateSessionForTenant(ctx, f.issuer, "Administrator", f.tenant); !errors.Is(e, denied) {
		t.Fatal("case-insensitive external identity", e)
	}
	if _, e = f.store.CreateSessionForTenant(ctx, f.issuer, "administrator", uuid.New()); !errors.Is(e, denied) {
		t.Fatal("tenant fallback", e)
	}
	if _, e = f.store.Provision(ctx, f.tenant, f.issuer, "administrator", "Administrator", []string{"platform_viewer"}); !errors.Is(e, conflict) {
		t.Fatal("operator overwrote existing roles", e)
	}
	if u, e := f.store.Provision(ctx, f.tenant, f.issuer, "administrator", "Administrator", []string{"platform_admin", "tenant_admin"}); e != nil || u != f.user {
		t.Fatal("provision replay", e)
	}
	v, e := f.store.Resolve(ctx, f.raw)
	if e != nil || v.UserID != f.user || v.TenantID != f.tenant {
		t.Fatal(v, e)
	}
	verifier, e := mysqlidentity.NewVerifier(f.db, f.database)
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := verifier.Resolve(ctx, f.raw)
	if e != nil || resolved.ActorID != f.user {
		t.Fatal("existing decision session contract rejected issued session", e)
	}
	var stored []byte
	if e = f.db.QueryRow("SELECT digest FROM "+f.store.table("identity_sessions")+" WHERE id=?", id(v.ID)).Scan(&stored); e != nil || string(stored) == f.raw || len(stored) != 32 {
		t.Fatal("session digest storage", e)
	}
}

func TestMySQLSessionsRevocationAndTenantIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second, e := f.store.CreateSessionForTenant(ctx, f.issuer, "administrator", f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.store.Resolve(ctx, second)
	if e != nil {
		t.Fatal(e)
	}
	items, e := f.store.Sessions(ctx, f.raw, uuid.Nil)
	if e != nil || len(items) != 2 {
		t.Fatal(items, e)
	}
	other := uuid.New()
	if e = f.store.RegisterTenant(ctx, other); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.Provision(ctx, other, f.issuer, "administrator", "Administrator", []string{"tenant_admin"}); e != nil {
		t.Fatal(e)
	}
	foreign, e := f.store.CreateSessionForTenant(ctx, f.issuer, "administrator", other)
	if e != nil {
		t.Fatal(e)
	}
	fv, e := f.store.Resolve(ctx, foreign)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.RevokeSessions(ctx, f.raw, fv.ID, false); !errors.Is(e, denied) {
		t.Fatal("cross-tenant revocation", e)
	}
	if current, e := f.store.RevokeSessions(ctx, f.raw, v.ID, false); e != nil || current {
		t.Fatal(current, e)
	}
	if _, e = f.store.Resolve(ctx, second); !errors.Is(e, denied) {
		t.Fatal("revocation not immediate", e)
	}
	if _, e = f.store.Resolve(ctx, foreign); e != nil {
		t.Fatal("foreign session revoked", e)
	}
	if current, e := f.store.RevokeSessions(ctx, f.raw, uuid.Nil, true); e != nil || !current {
		t.Fatal(current, e)
	}
	if _, e = f.store.Resolve(ctx, f.raw); !errors.Is(e, denied) {
		t.Fatal(e)
	}
	if e = f.store.Logout(ctx, f.raw); e != nil {
		t.Fatal("logout replay", e)
	}
	if e = f.store.Logout(ctx, "invalid"); e != nil {
		t.Fatal(e)
	}
}

func TestMySQLLogoutRemainsFinalAcrossTenantReactivation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, "UPDATE "+f.store.table("identity_tenants")+" SET active=FALSE WHERE id=?", id(f.tenant))
	if e := f.store.Logout(ctx, f.raw); e != nil {
		t.Fatal(e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_tenants")+" SET active=TRUE WHERE id=?", id(f.tenant))
	if _, e := f.store.Resolve(ctx, f.raw); !errors.Is(e, denied) {
		t.Fatal("logout credential became valid after reactivation", e)
	}
	var count int
	if e := f.db.QueryRow("SELECT COUNT(*) FROM "+f.store.table("identity_audit_events")+" WHERE tenant_id=? AND action='session.revoked'", id(f.tenant)).Scan(&count); e != nil || count != 1 {
		t.Fatal("logout audit", count, e)
	}
	if e := f.store.Logout(ctx, f.raw); e != nil {
		t.Fatal("logout replay", e)
	}
}

func TestMySQLSessionExpiryRenewalAndReadOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v, e := f.store.Resolve(ctx, f.raw)
	if e != nil {
		t.Fatal(e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_sessions")+" SET idle_until=TIMESTAMPADD(MINUTE,2,SYSDATE(6)),read_only=TRUE WHERE id=?", id(v.ID))
	if _, e = f.store.Resolve(ctx, f.raw); e != nil {
		t.Fatal("read-only session cannot resolve", e)
	}
	if e = f.store.ChangeMember(ctx, f.raw, f.user, 1, []string{"tenant_admin"}, true); !errors.Is(e, denied) {
		t.Fatal("read-only mutation", e)
	}
	var minutes int
	if e = f.db.QueryRow("SELECT TIMESTAMPDIFF(MINUTE,SYSDATE(6),idle_until) FROM "+f.store.table("identity_sessions")+" WHERE id=?", id(v.ID)).Scan(&minutes); e != nil || minutes < 28 {
		t.Fatal("idle renewal", minutes, e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_sessions")+" SET idle_until=TIMESTAMPADD(SECOND,1,created_at),read_only=FALSE WHERE id=?", id(v.ID))
	time.Sleep(1100 * time.Millisecond)
	if _, e = f.store.Resolve(ctx, f.raw); !errors.Is(e, denied) {
		t.Fatal("idle expiry", e)
	}
	newRaw, e := f.store.CreateSessionForTenant(ctx, f.issuer, "administrator", f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_tenants")+" SET active=FALSE WHERE id=?", id(f.tenant))
	if _, e = f.store.Resolve(ctx, newRaw); !errors.Is(e, denied) {
		t.Fatal("inactive tenant", e)
	}
}

func TestMySQLLastAdministratorConcurrencyAndAuditRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second, e := f.store.Provision(ctx, f.tenant, f.issuer, "second-admin", "Second", []string{"tenant_admin"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := f.store.CreateSessionForTenant(ctx, f.issuer, "second-admin", f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var wins atomic.Int32
	for _, item := range []struct {
		raw  string
		user uuid.UUID
	}{{f.raw, f.user}, {raw, second}} {
		wg.Add(1)
		go func(raw string, user uuid.UUID) {
			defer wg.Done()
			e := f.store.ChangeMember(ctx, raw, user, 1, []string{"platform_viewer"}, true)
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, conflict) {
				t.Error(e)
			}
		}(item.raw, item.user)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("last-admin race", wins.Load())
	}
	var admins int
	if e = f.db.QueryRow("SELECT COUNT(*) FROM "+f.store.table("identity_memberships")+" WHERE tenant_id=? AND active AND FIND_IN_SET('tenant_admin',roles)>0", id(f.tenant)).Scan(&admins); e != nil || admins != 1 {
		t.Fatal(admins, e)
	}
	mustExec(t, f, "CREATE TRIGGER "+f.store.schema+".auth_test_audit_fault BEFORE INSERT ON "+f.store.table("identity_audit_events")+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated audit fault'")
	var before, after int
	f.db.QueryRow("SELECT COUNT(*) FROM " + f.store.table("identity_sessions")).Scan(&before)
	if _, e = f.store.CreateSessionForTenant(ctx, f.issuer, "administrator", f.tenant); e == nil {
		t.Fatal("session committed without audit")
	}
	f.db.QueryRow("SELECT COUNT(*) FROM " + f.store.table("identity_sessions")).Scan(&after)
	if before != after {
		t.Fatal("partial session persistence")
	}
}

func TestMySQLInvitationLifecycleAndVerifiedAcceptance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	invite := uuid.New()
	roles := []string{"platform_viewer"}
	if _, e := f.invites.Create(ctx, f.raw, invite, "New@EXAMPLE.test", roles); e != nil {
		t.Fatal(e)
	}
	token := invitationToken(t, f, invite)
	if _, e := f.invites.Create(ctx, f.raw, invite, "New@example.test", roles); e != nil {
		t.Fatal("create replay", e)
	}
	if _, e := f.invites.Create(ctx, f.raw, invite, "Other@example.test", roles); !errors.Is(e, conflict) {
		t.Fatal("key conflict", e)
	}
	if _, e := f.invites.Create(ctx, f.raw, uuid.New(), "New@example.test", roles); !errors.Is(e, conflict) {
		t.Fatal("pending email uniqueness", e)
	}
	if _, e := f.invites.Begin(ctx, token); e != nil {
		t.Fatal(e)
	}
	identity := oidcprovider.Identity{Issuer: f.issuer, Subject: "new-person", Name: "New person", Email: "New@example.test", EmailVerified: false}
	if _, e := f.invites.Accept(ctx, sessioncredential.Digest(token), identity); !errors.Is(e, denied) {
		t.Fatal("unverified email", e)
	}
	identity.EmailVerified = true
	identity.Email = "new@example.test"
	if _, e := f.invites.Accept(ctx, sessioncredential.Digest(token), identity); !errors.Is(e, denied) {
		t.Fatal("email local-part case", e)
	}
	identity.Email = "New@example.test"
	raw, e := f.invites.Accept(ctx, sessioncredential.Digest(token), identity)
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.store.Resolve(ctx, raw)
	if e != nil || v.TenantID != f.tenant || !slices.Equal(v.Roles, roles) {
		t.Fatal(v, e)
	}
	if _, e = f.invites.Accept(ctx, sessioncredential.Digest(token), identity); !errors.Is(e, denied) {
		t.Fatal("invitation replay granted another session", e)
	}
	var status, state string
	var payload []byte
	if e = f.db.QueryRow("SELECT i.status,d.state,d.payload FROM "+f.store.table("identity_invitations")+" i JOIN "+f.store.table("identity_invitation_delivery")+" d ON d.invitation_id=i.id WHERE i.id=?", id(invite)).Scan(&status, &state, &payload); e != nil || status != "accepted" || state != "cancelled" || payload != nil {
		t.Fatal(status, state, e)
	}
	if _, e = f.invites.Create(ctx, raw, uuid.New(), "Nobody@example.test", roles); !errors.Is(e, denied) {
		t.Fatal("viewer invitation", e)
	}
}

type mailFunc func(context.Context, string, string, string) error

func (fn mailFunc) Send(ctx context.Context, to, link, key string) error {
	return fn(ctx, to, link, key)
}
func TestMySQLInvitationWorkerRetriesFencesAndRecovery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	invite := uuid.New()
	if _, e := f.invites.Create(ctx, f.raw, invite, "Worker@example.test", []string{"platform_viewer"}); e != nil {
		t.Fatal(e)
	}
	oldToken := invitationToken(t, f, invite)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := f.invites.DeliverOne(ctx, mailFunc(func(context.Context, string, string, string) error { close(started); <-release; return nil }))
		done <- e
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not send")
	}
	if e := f.invites.Change(ctx, f.raw, invite, 1, "cancel"); e != nil {
		t.Fatal("network call held invitation SQL locks", e)
	}
	close(release)
	if e := <-done; !errors.Is(e, ErrDeliveryFenced) {
		t.Fatal("stale completion", e)
	}
	if _, e := f.invites.Begin(ctx, oldToken); !errors.Is(e, denied) {
		t.Fatal("cancelled token", e)
	}
	invite = uuid.New()
	if _, e := f.invites.Create(ctx, f.raw, invite, "Retry@example.test", []string{"platform_viewer"}); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 5; n++ {
		mustExec(t, f, "UPDATE "+f.store.table("identity_invitation_delivery")+" SET next_attempt=TIMESTAMPADD(MINUTE,-1,SYSDATE(6)) WHERE invitation_id=?", id(invite))
		if claimed, e := f.invites.DeliverOne(ctx, mailFunc(func(context.Context, string, string, string) error { return errors.New("isolated SMTP failure") })); e != nil || !claimed {
			t.Fatal(claimed, e)
		}
	}
	var state string
	var attempts int
	var payload []byte
	if e := f.db.QueryRow("SELECT state,attempts,payload FROM "+f.store.table("identity_invitation_delivery")+" WHERE invitation_id=?", id(invite)).Scan(&state, &attempts, &payload); e != nil || state != "failed" || attempts != 5 || payload != nil {
		t.Fatal(state, attempts, e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_invitation_delivery")+" SET requested_at=TIMESTAMPADD(MINUTE,-2,SYSDATE(6)) WHERE invitation_id=?", id(invite))
	if e := f.invites.Change(ctx, f.raw, invite, 1, "resend"); e != nil {
		t.Fatal(e)
	}
	mustExec(t, f, "UPDATE "+f.store.table("identity_invitation_delivery")+" SET state='sending',lease_id=?,lease_until=TIMESTAMPADD(MINUTE,-1,SYSDATE(6)) WHERE invitation_id=?", id(uuid.New()), id(invite))
	var sent atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.invites.DeliverOne(ctx, mailFunc(func(context.Context, string, string, string) error { sent.Add(1); return nil }))
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if sent.Load() != 1 {
		t.Fatal("competing recovery sends", sent.Load())
	}
}
