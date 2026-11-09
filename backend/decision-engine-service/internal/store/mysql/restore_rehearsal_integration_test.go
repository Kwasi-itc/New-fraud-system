package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	casefixture "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/integrationtest"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlworkflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/workflowreceipt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlrelease"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

func TestMySQLLogicalRestoreRetainsDecisionsJobsAndCaseReceipts(t *testing.T) {
	if os.Getenv("DECISION_RESTORE_REHEARSAL") != "1" {
		t.Skip("explicit disposable restore rehearsal required")
	}
	if os.Getenv("DECISION_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("DECISION_MYSQL_REQUIRE_INTEGRATION") != "1" {
		t.Fatal("private fixture required")
	}
	const work = "/backend/decision-engine-service/.restore-rehearsal"
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal("fresh exchange directory required", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(work); err != nil {
			t.Error(err)
		}
	})
	f, unit, _ := combinedFixture(t, true)
	if !strings.HasPrefix(f.database, "decision_target_") {
		t.Fatal("generated fixture required")
	}
	identityPlan := installIdentityOwner(t, f.platformFixture)
	seedIdentity(t, f.platformFixture, f.tenant, "platform_admin")
	installCaseOwner(t, f.platformFixture)
	casePlan := installCaseWorkflowOwner(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(f.database)
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	if err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error { return persistCombinedOutputs(ctx, f, s, d) }); err != nil {
		t.Fatal(err)
	}
	inbox := uuid.New()
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".case_manager_inboxes(id,tenant_id,name,active) VALUES(?,?,'restore',1)", mysqlstore.UUID(inbox), mysqlstore.UUID(f.tenant)); err != nil {
		t.Fatal(err)
	}
	const token = "restore-rehearsal-integration-credential-only"
	owner := httptest.NewServer(casefixture.MySQLWorkflowReceiptOwner(t, f.db, f.database, token, f.tenant))
	defer owner.Close()
	client, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: owner.URL, Token: token, AllowHTTP: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	payload, err := json.Marshal(map[string]any{"execution_id": id, "tenant_id": f.tenant, "decision_id": d.ID, "scenario_id": f.parent, "action_type": "create_case", "action_config": map[string]any{"inbox_id": inbox, "name": "Restored workflow"}})
	if err != nil {
		t.Fatal(err)
	}
	request := ports.DispatchRequest{Kind: ports.JobWorkflow, TenantID: f.tenant.String(), ResourceID: id.String(), DomainAttempt: 1, Payload: payload}
	bindWorkflowRequest(t, &request)
	accepted, err := client.Deliver(ctx, request)
	if err != nil || accepted.Outcome != ports.DispatchAccepted {
		t.Fatal(accepted, err)
	}
	before := restoreSnapshot(t, ctx, f.db, f.database)
	restored, cfg, finishRestore := prepareRestoreSchema(t, ctx, f.platformFixture, identityPlan, casePlan)
	defer restored.Close()
	// All fixture writes have stopped. The companion pinned MySQL client performs
	// a real mysqldump and imports it into the separate disposable server.
	if err := os.WriteFile(work+"/database.tmp", []byte(f.database+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(work+"/database.tmp", work+"/database"); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(work + "/done"); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("restore companion did not complete", ctx.Err())
		case <-ticker.C:
		}
	}
	finishRestore()
	relationships, err := mysqlrelease.VerifyRelationships(ctx, restored, f.database)
	if err != nil {
		t.Fatal("restored relationships", err)
	}
	t.Logf("verified %d restored foreign-key relationships", relationships)
	var sourceID, targetID string
	if err := f.db.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&targetID); err != nil || sourceID == targetID {
		t.Fatal("restore must use a different server", err)
	}
	after := restoreSnapshot(t, ctx, restored, f.database)
	if !reflect.DeepEqual(before, after) {
		for key, want := range before {
			if after[key] != want {
				t.Errorf("restore mismatch %s", key)
				if key == "TABLE:decision_engine_scoring_configs" || key == "PROCEDURE:decision_engine_resolve_exact_key" {
					t.Logf("source: %s\nrestored: %s", want, after[key])
				}
			}
		}
		t.Fatal("restored schema/data inventory differs")
	}
	// Accounts are deployment configuration, not part of a schema dump. Install
	// the reviewed case runtime grants separately and resume with that account.
	user := "case_restore_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	account, _ := mysqlstore.Identifier(user)
	if _, err := restored.ExecContext(ctx, "CREATE USER "+account+"@'%' IDENTIFIED BY 'isolated-case-restore-only'"); err != nil {
		t.Fatal(err)
	}
	grants, err := mysqlworkflow.RuntimeGrants(f.database, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if _, err := restored.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	cfg.User, cfg.Password = user, "isolated-case-restore-only"
	runtimeDB, err := mysqlstore.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	restarted := httptest.NewServer(casefixture.MySQLWorkflowReceiptOwner(t, runtimeDB, f.database, token, f.tenant))
	defer restarted.Close()
	recovery, err := workflowreceipt.New(workflowreceipt.Config{BaseURL: restarted.URL, Token: token, AllowHTTP: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got, err := recovery.Reconcile(ctx, request)
	if err != nil || got != accepted {
		t.Fatal("restored receipt not retained", got, err)
	}
	got, err = recovery.Deliver(ctx, request)
	if err != nil || got != accepted {
		t.Fatal("restored replay changed outcome", got, err)
	}
	if !reflect.DeepEqual(before, restoreSnapshot(t, ctx, restored, f.database)) {
		t.Fatal("replay changed restored data")
	}
	if _, err := runtimeDB.ExecContext(ctx, "DELETE FROM "+schema+".case_manager_intake_receipts"); err == nil {
		t.Fatal("restored runtime can delete retained receipts")
	}
	sum, err := os.ReadFile(work + "/snapshot.sha256")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("restored %d schema/data inventory entries on a different MySQL server; snapshot %s", len(before), strings.TrimSpace(string(sum)))
}

// Reinstall the exact reference-frozen owner plans, rather than accepting DDL
// re-rendered by mysqldump. Only this new private restore schema is modified.
// It remains offline while guards are removed for bulk import; all original
// guards and original migration ledgers are verified before runtime access.
func prepareRestoreSchema(t *testing.T, ctx context.Context, source platformFixture, identity, casePlan migrations.Plan) (*sql.DB, mysqlstore.Config, func()) {
	t.Helper()
	cfg := platformConfig("mysql", "root", "isolated-test-root-only")
	cfg.Address = "mysqlrestore:3306"
	admin, err := mysqlstore.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema, err := mysqlstore.Identifier(source.database)
	if err != nil || !strings.HasPrefix(source.database, "decision_target_") {
		t.Fatal("generated restore schema required", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal("new restore database required", err)
	}
	definer, err := mysqlstore.Identifier(source.definer)
	if err != nil || !strings.HasPrefix(source.definer, "de_f_") {
		t.Fatal("fixture definer required", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE USER "+definer+"@`localhost` ACCOUNT LOCK"); err != nil {
		t.Fatal(err)
	}
	cfg.Database = source.database
	db, err := mysqlstore.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var runners []*migrations.Runner
	var triggers []migrations.Step
	plans := []struct {
		owner string
		plan  migrations.Plan
	}{{"decision_engine", source.plan}, {"identity", identity}, {"case_manager", casePlan}}
	for _, p := range plans {
		runner, err := migrations.NewForOwner(db, p.owner, p.plan)
		if err != nil {
			t.Fatal(err)
		}
		if err := runner.Apply(ctx); err != nil {
			t.Fatal("restore schema install", p.owner, err)
		}
		runners = append(runners, runner)
		for _, step := range p.plan.Steps {
			if step.Kind == "TRIGGER" {
				triggers = append(triggers, step)
			}
		}
		ledger, _ := mysqlstore.Identifier(p.owner + "_schema_migrations")
		if _, err := db.ExecContext(ctx, "DELETE FROM "+schema+"."+ledger); err != nil {
			t.Fatal(err)
		}
	}
	grants, err := metadata.DispatchDefinerGrants(source.database, source.definer)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if _, err := db.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range triggers {
		name, err := mysqlstore.Identifier(step.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "DROP TRIGGER "+schema+"."+name); err != nil {
			t.Fatal(err)
		}
	}
	return db, cfg, func() {
		for _, step := range triggers {
			if _, err := db.ExecContext(ctx, step.SQL); err != nil {
				t.Fatal("restore guard", step.ID, err)
			}
		}
		for _, runner := range runners {
			if err := runner.Verify(ctx, nil); err != nil {
				t.Fatal("restored original lineage", err)
			}
		}
		var foreign, unique int
		if err := db.QueryRowContext(ctx, "SELECT @@session.foreign_key_checks,@@session.unique_checks").Scan(&foreign, &unique); err != nil || foreign != 1 || unique != 1 {
			t.Fatal("restored checks weakened", err)
		}
	}
}

// This inventory is test-only: exact definitions, row counts and full logical
// table checksums. It intentionally covers all co-located fixture owners and
// migration ledgers instead of sampling only decision rows.
func restoreSnapshot(t *testing.T, ctx context.Context, db *sql.DB, database string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT kind,name FROM (
 SELECT IF(table_type='VIEW','VIEW','TABLE') kind,table_name name FROM information_schema.tables WHERE table_schema=?
 UNION ALL SELECT 'TRIGGER',trigger_name FROM information_schema.triggers WHERE trigger_schema=?
 UNION ALL SELECT routine_type,routine_name FROM information_schema.routines WHERE routine_schema=?
) objects ORDER BY kind,name LIMIT 1001`, database, database, database)
	if err != nil {
		t.Fatal(err)
	}
	type object struct{ kind, name string }
	var objects []object
	for rows.Next() {
		var v object
		if err := rows.Scan(&v.kind, &v.name); err != nil {
			t.Fatal(err)
		}
		objects = append(objects, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(objects) == 0 || len(objects) > 1000 {
		t.Fatal("invalid fixture inventory size", len(objects))
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	out := map[string]string{}
	schema, _ := mysqlstore.Identifier(database)
	for _, v := range objects {
		definition, err := migrations.Definition(ctx, conn, database, v.kind, v.name)
		if err != nil {
			t.Fatal(v, err)
		}
		out[v.kind+":"+v.name] = definition
		if v.kind == "TABLE" {
			name, err := mysqlstore.Identifier(v.name)
			if err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+name).Scan(&count); err != nil {
				t.Fatal(err)
			}
			var table string
			var checksum sql.NullInt64
			if err := conn.QueryRowContext(ctx, "CHECKSUM TABLE "+schema+"."+name+" EXTENDED").Scan(&table, &checksum); err != nil || !checksum.Valid {
				t.Fatal("checksum unavailable", v, err)
			}
			raw, _ := json.Marshal([]int64{count, checksum.Int64})
			out["DATA:"+v.name] = string(raw)
		}
	}
	return out
}
