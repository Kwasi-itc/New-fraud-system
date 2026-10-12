package preflight

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestClassificationPreservesIdentity(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"", "blank"}, {" \t", "blank"}, {" alice ", "noncanonical"},
		{"alice", "unresolved"}, {"legacy_import", "unresolved"},
		{"11111111-1111-4111-8111-111111111111", "unresolved"},
	} {
		raw := tc.raw
		if got := classify(&raw); got != tc.want || raw != tc.raw {
			t.Fatalf("%q: %s", tc.raw, got)
		}
	}
	if classify(nil) != "absent" {
		t.Fatal("null must remain distinguishable from blank")
	}
}

func TestRejectUnsafeScope(t *testing.T) {
	valid := Request{TenantID: uuid.New(), Source: "case_event_actors", Limit: 1}
	for _, change := range []func(*Request){
		func(r *Request) { r.TenantID = uuid.Nil },
		func(r *Request) { r.Source = "case_events; DELETE FROM core.tenants" },
		func(r *Request) { r.Limit = 0 }, func(r *Request) { r.Limit = 1001 },
	} {
		r := valid
		change(&r)
		if r.Validate() == nil {
			t.Fatal("unsafe request accepted")
		}
	}
}

// Uses a newly created disposable DB; never applies migrations or fixtures to
// AUTH_PREFLIGHT_TEST_DATABASE_URL itself. The supplied role needs CREATEDB.
func testDB(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("AUTH_PREFLIGHT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AUTH_PREFLIGHT_TEST_DATABASE_URL required for real PostgreSQL tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test database connection failed")
	}
	name := "auth_preflight_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	var conn *pgx.Conn
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if conn != nil {
			_ = conn.Close(cleanup)
		}
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		_ = admin.Close(cleanup)
	})
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database = name
	conn, err = pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "screening-service", "case-manager-service"} {
		files, err := filepath.Glob(filepath.Join("..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(files) == 0 {
			t.Fatalf("missing migrations: %s", owner)
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = conn.Exec(ctx, string(data)); err != nil {
				t.Fatalf("migration %s: %v", file, err)
			}
		}
	}
	return conn
}

func TestPostgresInventory(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	tenant, other, inbox, caseID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{tenant, other} {
		if _, err := conn.Exec(ctx, `INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Inventory fixture',$2,'active',now(),now())`, id, "tenant_"+strings.ReplaceAll(id.String(), "-", "")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(ctx, `INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Inventory',now(),now())`, inbox, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Inventory','pending','unset','decision',now(),now())`, caseID, tenant, inbox); err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002"), uuid.MustParse("00000000-0000-0000-0000-000000000003")}
	for i, subject := range []any{nil, "legacy_import", " alice "} {
		if _, err := conn.Exec(ctx, `INSERT INTO case_manager.case_events(id,tenant_id,case_id,user_id,event_type,new_value,previous_value,created_at) VALUES($1,$2,$3,$4,'case_assigned','new-investigator','old-investigator',now())`, ids[i], tenant, caseID, subject); err != nil {
			t.Fatal(err)
		}
	}
	// Every allowlisted source must match real owner migrations, even empty ones.
	for _, name := range Sources() {
		if _, err := ReadPage(ctx, conn, Request{TenantID: tenant, Source: name, Limit: 10}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	p, err := ReadPage(ctx, conn, Request{TenantID: tenant, Source: "case_event_actors", Limit: 2})
	if err != nil || !p.HasMore || p.NextAfter == nil || *p.NextAfter != ids[1] || len(p.References) != 2 {
		t.Fatalf("first page: %+v %v", p, err)
	}
	if p.References[0].Subject != nil || p.References[1].Classification != "unresolved" {
		t.Fatal("null/synthetic attribution changed")
	}
	p, err = ReadPage(ctx, conn, Request{TenantID: tenant, Source: "case_event_actors", Limit: 2, After: p.NextAfter})
	if err != nil || p.HasMore || p.NextAfter != nil || len(p.References) != 1 || p.References[0].Classification != "noncanonical" || *p.References[0].Subject != " alice " {
		t.Fatalf("last page: %+v %v", p, err)
	}
	for _, name := range []string{"assignment_targets", "assignment_previous"} {
		p, err = ReadPage(ctx, conn, Request{TenantID: tenant, Source: name, Limit: 1})
		if err != nil || len(p.References) != 1 || p.References[0].Subject == nil {
			t.Fatalf("assignment history: %+v %v", p, err)
		}
	}
	p, err = ReadPage(ctx, conn, Request{TenantID: other, Source: "case_event_actors", Limit: 2})
	if err != nil || len(p.References) != 0 || p.HasMore {
		t.Fatalf("cross-tenant leak: %+v %v", p, err)
	}
	if _, err = ReadPage(ctx, conn, Request{TenantID: uuid.New(), Source: "case_event_actors", Limit: 2}); err == nil {
		t.Fatal("missing tenant accepted")
	}
	// A canceled operation must not return a success-shaped partial report.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = ReadPage(canceled, conn, Request{TenantID: tenant, Source: "case_event_actors", Limit: 2}); err == nil {
		t.Fatal("canceled read accepted")
	}
	// Missing migration/table is an explicit failure, not an empty inventory.
	if _, err = conn.Exec(ctx, "DROP TABLE screening.investigator_reviews"); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadPage(ctx, conn, Request{TenantID: tenant, Source: "screening_review_actors", Limit: 2}); err == nil {
		t.Fatal("missing source accepted")
	}
	var count int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM case_manager.case_events").Scan(&count); err != nil || count != 3 {
		t.Fatalf("inventory changed source data: %d %v", count, err)
	}
}
