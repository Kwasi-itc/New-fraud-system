package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestExplorationLinksPivotsPathsAndAuthorization(t *testing.T) {
	db := testDatabase(t, true)
	s, tenant, inbox, c := fixture(t, db)
	ctx := userContext(tenant, "analyst-1")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var schema string
	if err := db.QueryRow(ctx, `SELECT schema_name FROM core.tenants WHERE id=$1`, tenant).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	exec("CREATE SCHEMA " + pgx.Identifier{schema}.Sanitize())
	customers, transactions := uuid.New(), uuid.New()
	customerID, device, transactionID, customerFK := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, tab := range []struct {
		id   uuid.UUID
		name string
	}{{customers, "customer"}, {transactions, "transaction"}} {
		exec(`INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,$3,now(),now())`, tab.id, tenant, tab.name)
	}
	for _, f := range []struct {
		id, table uuid.UUID
		name      string
	}{{customerID, customers, "object_id"}, {device, customers, "device_id"}, {transactionID, transactions, "object_id"}, {customerFK, transactions, "customer_id"}} {
		exec(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,$4,'String',now(),now())`, f.id, tenant, f.table, f.name)
	}
	cq := pgx.Identifier{schema, "customer"}.Sanitize()
	tq := pgx.Identifier{schema, "transaction"}.Sanitize()
	exec("CREATE TABLE " + cq + " (object_id text PRIMARY KEY,device_id text,internal_secret text)")
	exec("CREATE TABLE " + tq + " (object_id text PRIMARY KEY,customer_id text)")
	exec("INSERT INTO " + cq + " VALUES ('customer-1','device-A','hidden'),('customer-2','device-A','hidden'),('unrelated','device-B','hidden'),('empty','',NULL)")
	exec("INSERT INTO " + tq + " SELECT 'tx-'||lpad(n::text,3,'0'),'customer-1' FROM generate_series(1,60)n")
	exec("INSERT INTO " + tq + " VALUES ('foreign-record','unrelated')")
	exec("CREATE INDEX ON " + tq + " (customer_id,object_id)")
	exec("CREATE INDEX ON " + cq + " (device_id,object_id)")
	link, pivot, pathPivot, nav := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO core.model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES($1,$2,'customer',$3,$4,$5,$6,now())`, link, tenant, customers, customerID, transactions, customerFK)
	exec(`INSERT INTO core.model_pivots(id,tenant_id,base_table_id,field_id,created_at) VALUES($1,$2,$3,$4,now())`, pivot, tenant, customers, device)
	exec(`INSERT INTO core.model_pivots(id,tenant_id,base_table_id,path_link_ids,created_at) VALUES($1,$2,$3,$4,now())`, pathPivot, tenant, transactions, []uuid.UUID{link})
	exec(`INSERT INTO core.navigation_options(id,tenant_id,source_table_id,source_field_id,target_table_id,filter_field_id,ordering_field_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,now())`, nav, tenant, customers, customerID, transactions, customerFK, transactionID)
	decision := seedDecision(t, db, tenant)
	if _, err := s.AddDecision(ctx, service.AddDecisionInput{TenantID: tenant, CaseID: c.ID, DecisionID: decision}, nil); err != nil {
		t.Fatal(err)
	}
	in := casepkg.ExplorationRequest{DecisionID: decision}
	result, err := s.Explore(ctx, tenant, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if result.Table != "customer" || strings.Contains(string(result.Current.Data), "internal_secret") || len(result.Connections) != 3 {
		t.Fatalf("record or metadata: %+v", result)
	}
	in.Connection = "reverse:" + link.String()
	seen := map[string]bool{}
	for {
		result, err = s.Explore(ctx, tenant, c.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Records) > 25 {
			t.Fatal("unbounded page")
		}
		for _, rec := range result.Records {
			if seen[rec.ObjectID] {
				t.Fatal("duplicate page")
			}
			seen[rec.ObjectID] = true
		}
		if result.Next == "" {
			break
		}
		in.After = result.Next
	}
	if len(seen) != 60 || seen["foreign-record"] {
		t.Fatal("incorrect relationship pagination", len(seen))
	}
	in.After = ""
	in.Connection = "pivot:" + pivot.String()
	result, err = s.Explore(ctx, tenant, c.ID, in)
	if err != nil || len(result.Records) != 1 || result.Records[0].ObjectID != "customer-2" {
		t.Fatal("shared pivot", err, result)
	}
	in.Connection = "navigation:" + nav.String()
	result, err = s.Explore(ctx, tenant, c.ID, in)
	if err != nil || len(result.Records) != 25 {
		t.Fatal("navigation", err)
	}
	in.Path = []casepkg.ExplorationHop{{Connection: "reverse:" + link.String(), ObjectID: "tx-001"}}
	transactionDecision := seedDecision(t, db, tenant)
	exec(`UPDATE core.decisions SET object_type='transaction',object_id='tx-001' WHERE id=$1`, transactionDecision)
	if _, err = s.AttachExploredDecision(ctx, tenant, c.ID, casepkg.ExplorationAttach{ExplorationRequest: in, TargetDecisionID: transactionDecision}); err != nil {
		t.Fatal("attach discovered transaction decision", err)
	}
	in.Connection = "pivot:" + pathPivot.String()
	result, err = s.Explore(ctx, tenant, c.ID, in)
	if err != nil || result.Table != "transaction" || len(result.Records) != 1 || result.Records[0].ObjectID != "customer-1" {
		t.Fatal("path pivot", err, result)
	}
	in.Path = append(in.Path, casepkg.ExplorationHop{Connection: in.Connection, ObjectID: "customer-1"})
	in.Connection = ""
	result, err = s.Explore(ctx, tenant, c.ID, in)
	if err != nil || result.Table != "customer" {
		t.Fatal("multi-hop", err)
	}
	in.Path = []casepkg.ExplorationHop{{Connection: "reverse:" + link.String(), ObjectID: "foreign-record"}}
	if _, err = s.Explore(ctx, tenant, c.ID, in); !errors.Is(err, casepkg.ErrNotFound) {
		t.Fatal("forged target", err)
	}
	in.Path[0].Connection = "reverse:" + uuid.NewString()
	if _, err = s.Explore(ctx, tenant, c.ID, in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("forged edge", err)
	}
	in.Path = nil
	for _, denied := range []context.Context{userContext(tenant, "outsider"), userContext(uuid.New(), "analyst-1"), serviceContext(tenant)} {
		if _, err = s.Explore(denied, tenant, c.ID, in); err == nil {
			t.Fatal("unauthorized explorer")
		}
	}
	in.DecisionID = seedDecision(t, db, tenant)
	if _, err = s.Explore(ctx, tenant, c.ID, in); err == nil {
		t.Fatal("unlinked decision")
	}
	in.DecisionID = decision
	// Malformed cross-tenant metadata cannot introduce a traversal endpoint.
	other := uuid.New()
	seedTenant(t, db, other)
	foreignTable, foreignField := uuid.New(), uuid.New()
	exec(`INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'foreign',now(),now())`, foreignTable, other)
	exec(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,'object_id','String',now(),now())`, foreignField, other, foreignTable)
	badLink := uuid.New()
	exec(`INSERT INTO core.model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES($1,$2,'bad',$3,$4,$5,$6,now())`, badLink, tenant, foreignTable, foreignField, customers, customerID)
	in.Connection = "link:" + badLink.String()
	if _, err = s.Explore(ctx, tenant, c.ID, in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("foreign metadata", err)
	}
	in.Connection = ""
	// Metadata deletion/archive is honored on replay of a previously valid trail.
	exec(`UPDATE core.model_fields SET archived=true WHERE id=$1`, device)
	result, err = s.Explore(ctx, tenant, c.ID, in)
	if err != nil || strings.Contains(string(result.Current.Data), "device_id") {
		t.Fatal("archived field exposure", err)
	}
	exec(`DELETE FROM core.model_links WHERE id=$1`, link)
	in.Path = []casepkg.ExplorationHop{{Connection: "reverse:" + link.String(), ObjectID: "tx-001"}}
	if _, err = s.Explore(ctx, tenant, c.ID, in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("stale path", err)
	}
	in.Path = nil
	// Exercise real platform revocation rather than only legacy role principals.
	schemaSQL, err := os.ReadFile("../../../../auth-service/internal/platform/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(schemaSQL))
	user, sid := uuid.New(), uuid.New()
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Explorer')`, user)
	exec(`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['case_manager_admin'])`, user, tenant)
	exec(`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,'explorer',now()+interval '1 hour',now()+interval '1 hour')`, sid, user, tenant)
	t.Setenv("USER_REQUIRE_SESSION", "true")
	verified := access.WithPrincipal(context.Background(), access.Principal{Kind: access.User, Subject: user.String(), TenantID: tenant, Admin: true, SessionID: sid, AuthzVersion: 1})
	if _, err = s.Explore(verified, tenant, c.ID, in); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, sid)
	if _, err = s.AttachExploredDecision(verified, tenant, c.ID, casepkg.ExplorationAttach{ExplorationRequest: in, TargetDecisionID: decision}); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("revoked attachment", err)
	}
	if _, err = s.Explore(verified, tenant, c.ID, in); !errors.Is(err, casepkg.ErrForbidden) {
		t.Fatal("revoked session", err)
	}
	_ = inbox
}

func TestExplorationRepresentativeIndexedQueryPlan(t *testing.T) {
	db := testDatabase(t, true)
	_, tenant, _, _ := fixture(t, db)
	ctx := context.Background()
	schema := "explorer_plan_" + strings.ReplaceAll(tenant.String(), "-", "")
	q := pgx.Identifier{schema, "records"}.Sanitize()
	for _, sql := range []string{"CREATE SCHEMA " + pgx.Identifier{schema}.Sanitize(), "CREATE TABLE " + q + " (object_id text PRIMARY KEY,customer_id text)", "INSERT INTO " + q + " SELECT lpad(n::text,8,'0'),'customer-'||(n%1000)::text FROM generate_series(1,50000)n", "CREATE INDEX explorer_connection_idx ON " + q + " (customer_id,object_id)", "ANALYZE " + q} {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	var plan json.RawMessage
	if err := db.QueryRow(ctx, fmt.Sprintf(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT DISTINCT s1.object_id FROM %s s0 JOIN %s s1 ON s0.customer_id=s1.customer_id AND s0.customer_id::text<>'' WHERE s0.object_id=$1 AND s1.object_id>$2 AND s1.object_id<>$1 ORDER BY s1.object_id LIMIT 26`, q, q), "00000042", "").Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan), "explorer_connection_idx") {
		t.Fatalf("expected connection index: %s", plan)
	}
	t.Logf("50,000-row indexed relationship query: %s", plan)
}
