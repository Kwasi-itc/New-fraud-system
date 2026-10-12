package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Always create a fresh database. No reset helper ever touches the supplied DB.
func platformDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("AUTH_PREFLIGHT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set AUTH_PREFLIGHT_TEST_DATABASE_URL for disposable database tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "datamodel_auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close(ctx)
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
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close(ctx)
	})
	files, err := filepath.Glob("../migrations/metadata/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("owner migrations missing", err)
	}
	files = append(files, "../../../auth-service/internal/platform/schema.sql", "../../../auth-service/internal/platform/schema_v2.sql")
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	return db
}

func TestPlatformDataModelAuthorization(t *testing.T) {
	db := platformDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	tenant, other, user, sid, table, foreign, field, foreignField := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{tenant, other} {
		exec("INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'test',$2,'active',now(),now())", id, "tenant_"+strings.ReplaceAll(id.String(), "-", ""))
	}
	exec("INSERT INTO identity.users(id,display_name) VALUES($1,'Reader')", user)
	exec("INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['platform_viewer'])", tenant, user)
	exec("INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '30 minutes')", sid, []byte("test-digest"), tenant, user)
	exec("INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'objects',now(),now()),($3,$4,'foreign_objects',now(),now())", table, tenant, foreign, other)
	exec("INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,is_unique,created_at,updated_at) VALUES($1,$2,$3,'object_id','String',true,now(),now()),($4,$5,$6,'object_id','String',true,now(),now())", field, tenant, table, foreignField, other, foreign)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	verifier, err := investigator.NewVerifier(map[string][]byte{"test": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})}, "test-platform", "case-manager")
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{"iss": "test-platform", "aud": "case-manager", "sub": user.String(), "tenant_id": tenant.String(), "sid": sid.String(), "authz_version": 1, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "roles": []string{"platform_admin"}}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test"
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(slog.Default(), db, RouterConfig{AuthMode: "token", AuthToken: "private-machine-token", UserVerifier: verifier})
	request := func(method, path, body, credential string, want int) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
	}
	list := "/v1/tenants/" + tenant.String() + "/tables"
	request("GET", list, "", "", 401)
	request("GET", list, "", raw, 200)
	request("PATCH", "/v1/tables/"+table.String(), `{"alias":"denied"}`, raw, 403) // DB role overrides token role claim.
	request("GET", "/v1/tenants/"+other.String()+"/tables", "", raw, 403)
	request("GET", "/v1/tables/"+foreign.String()+"/fields", "", raw, 403)
	request("GET", "/v1/fields/"+foreignField.String()+"/enum-values", "", raw, 403)
	request("GET", "/v1/tenants", "", raw, 403)
	request("GET", "/v1/admin/reconcile", "", raw, 403)
	request("POST", "/v1/tenants/"+tenant.String()+"/provision", "{}", raw, 403)
	request("GET", list, "", "private-machine-token", 200)
	exec("UPDATE identity.memberships SET roles=ARRAY['tenant_admin','case_manager_admin'] WHERE user_id=$1", user)
	request("GET", list, "", raw, 403)
	exec("UPDATE identity.memberships SET roles=ARRAY['platform_admin'] WHERE user_id=$1", user)
	request("PATCH", "/v1/tables/"+table.String(), `{"alias":"allowed"}`, raw, 200)
	request("PATCH", "/v1/tables/"+foreign.String(), `{"alias":"denied"}`, raw, 403)
	request("POST", "/v1/tenants/"+tenant.String()+"/links", `{"name":"foreign_field","parent_table_id":"`+table.String()+`","child_table_id":"`+table.String()+`","parent_field_id":"`+foreignField.String()+`","child_field_id":"`+field.String()+`"}`, raw, 400)
	request("POST", "/v1/tenants/"+tenant.String()+"/pivots", `{"base_table_id":"`+table.String()+`","field_id":"`+foreignField.String()+`"}`, raw, 400)
	exec("UPDATE identity.memberships SET version=2 WHERE user_id=$1", user)
	request("GET", list, "", raw, 403)
	exec("UPDATE identity.memberships SET version=1 WHERE user_id=$1", user)

	p := investigator.Principal{Subject: user.String(), TenantID: tenant, SessionID: sid, AuthzVersion: 1}
	g := access.Grant{Principal: p, Write: true, Kind: "tableId", ID: table}
	// Revocation wins between request authorization and transaction entry.
	if err := access.Check(ctx, db, g, false); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE identity.sessions SET revoked_at=now() WHERE id=$1", sid)
	ran := false
	err = store.NewTransactionManager(db).Run(access.WithGrant(ctx, g), func(ports.MutationStore) error { ran = true; return nil })
	var denied *access.Error
	if ran || !errors.As(err, &denied) || denied.Status != 403 {
		t.Fatalf("revoked transaction ran=%v error=%v", ran, err)
	}
	request("GET", list, "", raw, 403)
	exec("UPDATE identity.sessions SET revoked_at=NULL WHERE id=$1", sid)
	exec("UPDATE identity.memberships SET active=false WHERE user_id=$1", user)
	request("GET", list, "", raw, 403)
	exec("UPDATE identity.memberships SET active=true WHERE user_id=$1", user)

	// An authorized mutation keeps revocation from committing until it finishes.
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := access.Check(ctx, tx, g, true); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = db.Exec(waitCtx, "UPDATE identity.memberships SET active=false WHERE user_id=$1", user)
	cancel()
	if err == nil {
		t.Fatal("suspension bypassed active mutation lock")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE identity.memberships SET active=false WHERE user_id=$1", user)
	request("GET", list, "", raw, 403)
	// An unavailable identity contract must not execute the handler.
	exec("DROP FUNCTION identity.authorize_data_model_v1(text,uuid,uuid,bigint,boolean,boolean)")
	request("GET", list, "", raw, 503)
}

func TestPlatformRoutePoliciesCoverRegisteredRoutes(t *testing.T) {
	router := NewRouter(slog.Default(), nil, RouterConfig{AuthMode: "token", AuthToken: "test"})
	serviceOnly := map[string]bool{"GET /v1/tenants": true, "POST /v1/tenants": true, "POST /v1/tenants/:tenantId/provision": true, "GET /v1/admin/reconcile": true}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if !strings.HasPrefix(route.Path, "/v1/") {
			continue
		}
		if _, ok := platformRoutePolicies[key]; !ok && !serviceOnly[key] {
			t.Errorf("route needs explicit authorization policy: %s", key)
		}
	}
}
