package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func fixture(t *testing.T) (Store, uuid.UUID) {
	return fixtureVersion(t, SchemaVersion)
}

func fixtureVersion(t *testing.T, version int) (Store, uuid.UUID) {
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
	if version == SchemaVersion {
		if err = s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err = s.Migrate(ctx); err != nil {
			t.Fatal("idempotent migration", err)
		}
	} else if version == 5 {
		if _, err = db.Exec(ctx, schema+schemaV2+schemaV3+schemaV4+schemaV5+schemaV5Seed); err != nil {
			t.Fatal(err)
		}
	} else if version == 6 {
		if _, err = db.Exec(ctx, schema+schemaV2+schemaV3+schemaV4+schemaV5+schemaV5Seed+schemaV6); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `UPDATE identity.capability_catalog SET client_access='' WHERE name IN ('models:read','scenarios:read','scenarios:write')`); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("unsupported fixture version")
	}
	tenant := uuid.New()
	if _, err = db.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Test',$2,'active',now(),now())`, tenant, "tenant_"+strings.ReplaceAll(tenant.String(), "-", "")); err != nil {
		t.Fatal(err)
	}
	return s, tenant
}

func TestMigrationEnablesClientScenarioDraftScopes(t *testing.T) {
	s, _ := fixtureVersion(t, 6)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("rerun after v7 upgrade: %v", err)
	}
	rows, err := s.DB.Query(context.Background(), `SELECT name,client_access FROM identity.capability_catalog WHERE name IN ('models:read','scenarios:read','scenarios:write','scenarios:publish') ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, clientAccess string
		if err := rows.Scan(&name, &clientAccess); err != nil {
			t.Fatal(err)
		}
		got[name] = clientAccess
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models:read", "scenarios:read", "scenarios:write"} {
		if got[name] != "normal" {
			t.Errorf("%s client access=%q, want normal", name, got[name])
		}
	}
	if got["scenarios:publish"] != "" {
		t.Errorf("scenario publication client access=%q, want unavailable", got["scenarios:publish"])
	}
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
	admin, adminRaw := provision(t, s, tenant, "admin", "tenant_admin")
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

func TestDescribeLoginConsumeFailure(t *testing.T) {
	for _, test := range []struct {
		name                               string
		stateExists, binding, issuer, live bool
		want                               string
	}{
		{name: "unknown or consumed state", want: "state not found (invalid state, expired and removed, or already consumed)"},
		{name: "invalid binding", stateExists: true, issuer: true, live: true, want: "binding does not match"},
		{name: "invalid issuer", stateExists: true, binding: true, live: true, want: "issuer does not match"},
		{name: "expired", stateExists: true, binding: true, issuer: true, want: "login transaction expired"},
		{name: "multiple mismatches", stateExists: true, live: true, want: "binding does not match; issuer does not match"},
		{name: "concurrent consume", stateExists: true, binding: true, issuer: true, live: true, want: "transaction was consumed or changed concurrently"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := describeLoginConsumeFailure(test.stateExists, test.binding, test.issuer, test.live)
			if got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
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
	key, publicKey := clientTestKey(t)
	verifier, proxyToken := testBFFAndPlatform(t, "test", publicKey)
	current, err := s.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	humanToken := testHumanToken(t, "test", key, current)
	server := httptest.NewServer(Server{Store: s, Verifier: verifier, Key: key, KeyID: "test", Issuer: auth.PlatformIssuer, Audience: auth.PlatformAudience, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}.Handler())
	defer server.Close()
	for _, tc := range []struct {
		path, token, body string
		status            int
	}{{"internal/session", "", `{}`, 401}, {"internal/session", proxyToken, `{"session":"bad"}`, 401}, {"internal/members/list", proxyToken, `{"session":"` + raw + `"}`, 404}, {"v1/account/profile", proxyToken, `{}`, 403}, {"v1/tenants/" + tenant.String() + "/members", humanToken, `{}`, 403}} {
		method := http.MethodPost
		if strings.HasSuffix(tc.path, "/members") || strings.HasSuffix(tc.path, "/profile") {
			method = http.MethodGet
		}
		req, _ := http.NewRequest(method, server.URL+"/"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Content-Type", "application/json")
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
	req.Header.Set("Authorization", "Bearer "+proxyToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil || out["access_token"] == nil {
		t.Fatal("missing private access token", err)
	}
	req, _ = http.NewRequest("GET", server.URL+"/v1/account/profile", nil)
	req.Header.Set("Authorization", "Bearer "+humanToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("human profile route status %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", server.URL+"/internal/session", strings.NewReader(`{"session":"`+raw+`"}`))
	req.Header.Set("Authorization", "Bearer "+humanToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("human token accessed bootstrap route: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("PUT", server.URL+"/v1/account/profile", strings.NewReader(`{"session":"`+raw+`","name":"Forged","version":1}`))
	req.Header.Set("Authorization", "Bearer "+humanToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("caller-supplied session was not rejected: %d", resp.StatusCode)
	}
	if err = s.Logout(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest("GET", server.URL+"/v1/account/profile", nil)
	req.Header.Set("Authorization", "Bearer "+humanToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("revoked session still authorized profile route: %d", resp.StatusCode)
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
