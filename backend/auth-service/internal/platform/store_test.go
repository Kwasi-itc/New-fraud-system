package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (Store, uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("AUTH_PREFLIGHT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set AUTH_PREFLIGHT_TEST_DATABASE_URL for disposable PostgreSQL tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "auth_platform_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 5
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)")
		if err != nil {
			t.Error(err)
		}
		admin.Close(ctx)
	})
	files, err := filepath.Glob("../../../data-model-service/internal/migrations/metadata/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("missing owner migrations")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	s := Store{db}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal("idempotent migration", err)
	}
	tenant := uuid.New()
	if _, err = db.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Test',$2,'active',now(),now())`, tenant, "tenant_"+strings.ReplaceAll(tenant.String(), "-", "")); err != nil {
		t.Fatal(err)
	}
	return s, tenant
}
func provision(t *testing.T, s Store, tenant uuid.UUID, subject string, roles ...string) (uuid.UUID, string) {
	t.Helper()
	id, err := s.Provision(context.Background(), tenant, "https://provider.test", subject, subject, roles)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.CreateSession(context.Background(), "https://provider.test", subject)
	if err != nil {
		t.Fatal(err)
	}
	return id, raw
}
func allowed(t *testing.T, s Store, v Session, admin bool) bool {
	t.Helper()
	var yes bool
	if err := s.DB.QueryRow(context.Background(), `SELECT identity.authorize_case_v1($1,$2,$3,$4,$5,false)`, v.UserID.String(), v.TenantID, v.ID, v.Version, admin).Scan(&yes); err != nil {
		t.Fatal(err)
	}
	return yes
}

func TestPlatformSessionsAndPermissions(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	admin, adminRaw := provision(t, s, tenant, "admin", "tenant_admin", "case_manager_admin")
	user, userRaw := provision(t, s, tenant, "analyst", "case_investigator")
	if _, err := s.CreateSession(ctx, "https://provider.test", "unknown"); !errors.Is(err, ErrDenied) {
		t.Fatal("unknown identity accepted", err)
	}
	if _, err := s.CreateSession(ctx, "https://replacement.test", "analyst"); !errors.Is(err, ErrDenied) {
		t.Fatal("issuer alias accepted", err)
	}
	same, err := s.Provision(ctx, tenant, "https://provider.test", "analyst", "changed display", []string{"case_investigator"})
	if err != nil || same != user {
		t.Fatal("provision replay not stable")
	}
	if _, err = s.Provision(ctx, tenant, "https://provider.test", "analyst", "Analyst", []string{"tenant_admin"}); !errors.Is(err, ErrConflict) {
		t.Fatal("CLI silently elevated existing account")
	}
	if _, err = s.Members(ctx, userRaw, uuid.Nil); !errors.Is(err, ErrDenied) {
		t.Fatal("investigator read admin directory")
	}
	v, err := s.Resolve(ctx, userRaw)
	if err != nil || !allowed(t, s, v, false) || allowed(t, s, v, true) {
		t.Fatal("case role mismatch", err)
	}
	wrong := v
	wrong.TenantID = uuid.New()
	if allowed(t, s, wrong, false) {
		t.Fatal("cross tenant allowed")
	}
	if err = s.ChangeMember(ctx, userRaw, admin, 1, []string{"tenant_admin"}, false); !errors.Is(err, ErrDenied) {
		t.Fatal("investigator admin mutation")
	}
	if err = s.ChangeMember(ctx, adminRaw, admin, 1, []string{"case_manager_admin"}, true); !errors.Is(err, ErrConflict) {
		t.Fatal("last admin removal allowed", err)
	}
	if err = s.ChangeMember(ctx, adminRaw, user, 99, []string{"case_investigator"}, false); !errors.Is(err, ErrConflict) {
		t.Fatal("stale change accepted")
	}
	if err = s.ChangeMember(ctx, adminRaw, user, 1, []string{"case_investigator"}, false); err != nil {
		t.Fatal(err)
	}
	if allowed(t, s, v, false) {
		t.Fatal("old JWT remains authorized")
	}
	if _, err = s.Resolve(ctx, userRaw); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked cookie accepted")
	}
	if err = s.ChangeMember(ctx, adminRaw, user, 2, []string{"case_investigator"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, userRaw); !errors.Is(err, ErrDenied) {
		t.Fatal("reactivation restored old cookie")
	}
	fresh, err := s.CreateSession(ctx, "https://provider.test", "analyst")
	if err != nil {
		t.Fatal(err)
	}
	fv, _ := s.Resolve(ctx, fresh)
	if err = s.Logout(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if allowed(t, s, fv, false) {
		t.Fatal("logout JWT accepted")
	}
	if err = s.Logout(ctx, fresh); err != nil {
		t.Fatal("logout not idempotent")
	}
	tx, err := s.BeginLogin(ctx, "https://provider.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConsumeLogin(ctx, tx.State, secret(), "https://provider.test"); !errors.Is(err, ErrDenied) {
		t.Fatal("wrong browser binding accepted")
	}
	if _, err = s.ConsumeLogin(ctx, tx.State, tx.Binding, "https://provider.test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConsumeLogin(ctx, tx.State, tx.Binding, "https://provider.test"); !errors.Is(err, ErrDenied) {
		t.Fatal("login replay accepted")
	}
	var stored []byte
	if err = s.DB.QueryRow(ctx, "SELECT digest FROM identity.sessions WHERE id=$1", v.ID).Scan(&stored); err != nil || bytes.Equal(stored, []byte(userRaw)) {
		t.Fatal("raw session stored")
	}
}

func TestRevocationLocksAndRollback(t *testing.T) {
	s, tenant := fixture(t)
	ctx := context.Background()
	_, adminRaw := provision(t, s, tenant, "admin", "tenant_admin")
	user, raw := provision(t, s, tenant, "analyst", "case_investigator")
	v, _ := s.Resolve(ctx, raw)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var yes bool
	if err = tx.QueryRow(ctx, `SELECT identity.authorize_case_v1($1,$2,$3,$4,false,true)`, user.String(), tenant, v.ID, v.Version).Scan(&yes); err != nil || !yes {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.ChangeMember(ctx, adminRaw, user, 1, []string{"case_investigator"}, false) }()
	select {
	case err := <-done:
		t.Fatal("suspension passed active transaction", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revocation stuck")
	}
	if allowed(t, s, v, false) {
		t.Fatal("post-commit access allowed")
	}
	if _, err = s.DB.Exec(ctx, `CREATE FUNCTION identity.fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'test rollback'; END$$; CREATE TRIGGER fail_audit BEFORE INSERT ON identity.audit_events FOR EACH ROW EXECUTE FUNCTION identity.fail_audit()`); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeMember(ctx, adminRaw, user, 2, []string{"case_investigator"}, true); err == nil {
		t.Fatal("audit failure accepted")
	}
	var active bool
	var version int64
	if err = s.DB.QueryRow(ctx, "SELECT active,version FROM identity.memberships WHERE tenant_id=$1 AND user_id=$2", tenant, user).Scan(&active, &version); err != nil || active || version != 2 {
		t.Fatal("audit failure did not roll back")
	}
}

func TestConcurrentLastAdmin(t *testing.T) {
	s, tenant := fixture(t)
	a, ar := provision(t, s, tenant, "a", "tenant_admin")
	b, br := provision(t, s, tenant, "b", "tenant_admin")
	var wg sync.WaitGroup
	for _, v := range []struct {
		id  uuid.UUID
		raw string
	}{{a, ar}, {b, br}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.ChangeMember(context.Background(), v.raw, v.id, 1, []string{"case_investigator"}, true)
		}()
	}
	wg.Wait()
	var count int
	if err := s.DB.QueryRow(context.Background(), "SELECT count(*) FROM identity.memberships WHERE tenant_id=$1 AND active AND 'tenant_admin'=ANY(roles)", tenant).Scan(&count); err != nil || count != 1 {
		t.Fatalf("last-admin invariant: %d %v", count, err)
	}
}

func TestHTTPPermissions(t *testing.T) {
	s, tenant := fixture(t)
	_, raw := provision(t, s, tenant, "analyst", "case_investigator")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Server{Store: s, ProxyToken: strings.Repeat("x", 32), Key: key, KeyID: "test", Issuer: "platform", Audience: "case-manager", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}.Handler())
	defer server.Close()
	for _, tc := range []struct {
		path, token, body string
		status            int
	}{{"session", "", `{}`, 401}, {"session", strings.Repeat("x", 32), `{"session":"bad"}`, 401}, {"members/list", strings.Repeat("x", 32), `{"session":"` + raw + `","after":"00000000-0000-0000-0000-000000000000"}`, 403}, {"session", strings.Repeat("x", 32), `{"session":"` + raw + `","roles":["tenant_admin"]}`, 400}} {
		req, _ := http.NewRequest("POST", server.URL+"/internal/"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("%s: %d", tc.path, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("POST", server.URL+"/internal/session", strings.NewReader(`{"session":"`+raw+`"}`))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil || out["access_token"] == nil {
		t.Fatal("missing private access token", err)
	}
}

func TestMigrationRejectsNewerSchema(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if _, err := s.DB.Exec(ctx, "INSERT INTO identity.schema_migrations(version) VALUES($1)", SchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("older migration binary accepted a newer schema")
	}
}
