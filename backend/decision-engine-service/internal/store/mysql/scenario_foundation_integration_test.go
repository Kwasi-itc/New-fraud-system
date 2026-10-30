package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

// Extend the independently tested three-step platform lineage. Reference
// observations are made only in a generated disposable reference database.
func scenarioFoundationDB(t *testing.T, authoring ...bool) platformFixture {
	t.Helper()
	statements := metadata.ScenarioFoundationStatements
	bindPlan := metadata.ScenarioFoundationPlan
	definerGrants := metadata.ScenarioDefinerGrants
	if len(authoring) > 0 && authoring[0] {
		statements = metadata.AuthoringStatements
		bindPlan = metadata.AuthoringPlan
	}
	if len(authoring) > 1 && authoring[1] {
		statements = metadata.DecisionStatements
		bindPlan = metadata.DecisionPlan
		definerGrants = metadata.DecisionDefinerGrants
	}
	if len(authoring) > 2 && authoring[2] {
		statements = metadata.HistoryStatements
		bindPlan = metadata.HistoryPlan
		definerGrants = metadata.HistoryDefinerGrants
	}
	if len(authoring) > 3 && authoring[3] {
		statements = metadata.ConfigurationStatements
		bindPlan = metadata.ConfigurationPlan
		definerGrants = metadata.ConfigurationDefinerGrants
	}
	if len(authoring) > 4 && authoring[4] {
		statements = metadata.WorkflowStatements
		bindPlan = metadata.WorkflowPlan
		definerGrants = metadata.WorkflowDefinerGrants
	}
	if len(authoring) > 5 && authoring[5] {
		statements = metadata.ListsStatements
		bindPlan = metadata.ListsPlan
		definerGrants = metadata.ListsDefinerGrants
	}
	if len(authoring) > 6 && authoring[6] {
		statements = metadata.EffectsStatements
		bindPlan = metadata.EffectsPlan
		definerGrants = metadata.EffectsDefinerGrants
	}
	if len(authoring) > 7 && authoring[7] {
		statements = metadata.BrowserAuthoringStatements
		bindPlan = metadata.BrowserAuthoringPlan
		definerGrants = metadata.BrowserAuthoringDefinerGrants
	}
	if len(authoring) > 8 && authoring[8] {
		statements = metadata.EffectJobsStatements
		bindPlan = metadata.EffectJobsPlan
		definerGrants = metadata.EffectJobsDefinerGrants
	}
	if len(authoring) > 9 && authoring[9] {
		statements = metadata.ExecutionStatements
		bindPlan = metadata.ExecutionPlan
		definerGrants = metadata.ExecutionDefinerGrants
	}
	if len(authoring) > 10 && authoring[10] {
		statements = metadata.EffectCancellationStatements
		bindPlan = metadata.EffectCancellationPlan
		definerGrants = metadata.EffectCancellationDefinerGrants
	}
	if len(authoring) > 11 && authoring[11] {
		statements = metadata.DispatchStatements
		bindPlan = metadata.DispatchPlan
		definerGrants = metadata.DispatchDefinerGrants
	}
	f := platformTestDB(t)
	ctx := context.Background()
	definer := "de_f_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	account := "`" + definer + "`@`localhost`"
	if _, err := f.db.ExecContext(ctx, "CREATE USER "+account+" ACCOUNT LOCK"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	refName := "decision_foundation_ref_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	refSchema, _ := mysqlstore.Identifier(refName)
	if _, err := f.db.ExecContext(ctx, "CREATE DATABASE "+refSchema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), "DROP DATABASE "+refSchema); err != nil {
			t.Error(err)
		}
	})
	ref, err := mysqlstore.Open(ctx, platformConfig(refName, "root", "isolated-test-root-only"))
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	source, err := statements(refName, definer)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := statements(f.database, definer)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ref.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i, step := range source {
		if strings.HasPrefix(step.SQL, "ALTER ") {
			before, err := migrations.Definition(ctx, conn, refName, step.Kind, step.Name)
			if err != nil {
				t.Fatal(err)
			}
			reference[i].Before = strings.ReplaceAll(before, refName, f.database)
		}
		if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatalf("reference step %s: %v", step.Name, err)
		}
		after, err := migrations.Definition(ctx, conn, refName, step.Kind, step.Name)
		if err != nil {
			t.Fatal(err)
		}
		// Fixture-only rebinding of generated schema names, not runtime SQL rewriting.
		reference[i].After = strings.ReplaceAll(after, refName, f.database)
	}
	plan, err := bindPlan(f.database, definer, reference)
	if err != nil {
		t.Fatal(err)
	}
	if len(authoring) > 10 && authoring[10] {
		// Exercise the exact prior 140-step installation before appending the
		// cancellation objects, not only a fresh all-at-once fixture.
		prefix, err := metadata.ExecutionPlan(f.database, definer, reference[:140])
		if err != nil {
			t.Fatal(err)
		}
		prior, err := migrations.New(f.db, "decision_engine", prefix)
		if err != nil {
			t.Fatal(err)
		}
		if err := prior.Apply(ctx); err != nil {
			t.Fatal("140-step prefix install", err)
		}
		if err := prior.Verify(ctx, nil); err != nil {
			t.Fatal("140-step prefix verify", err)
		}
	}
	if len(authoring) > 11 && authoring[11] {
		prefix, err := metadata.EffectCancellationPlan(f.database, definer, reference[:145])
		if err != nil {
			t.Fatal(err)
		}
		prior, err := migrations.New(f.db, "decision_engine", prefix)
		if err != nil {
			t.Fatal(err)
		}
		if err := prior.Apply(ctx); err != nil {
			t.Fatal("145-step prefix install", err)
		}
		if err := prior.Verify(ctx, nil); err != nil {
			t.Fatal("145-step prefix verify", err)
		}
	}
	runner, err := migrations.New(f.db, "decision_engine", plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal("append platform upgrade", err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal("repeat upgrade", err)
	}
	if err := runner.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// Also prove the complete lineage installs from empty, not just the
	// append-only upgrade from the existing three platform steps.
	freshName := "decision_foundation_fresh_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	freshSchema, _ := mysqlstore.Identifier(freshName)
	if _, err := f.db.ExecContext(ctx, "CREATE DATABASE "+freshSchema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(context.Background(), "DROP DATABASE "+freshSchema); err != nil {
			t.Error(err)
		}
	})
	freshDB, err := mysqlstore.Open(ctx, platformConfig(freshName, "root", "isolated-test-root-only"))
	if err != nil {
		t.Fatal(err)
	}
	defer freshDB.Close()
	freshReference, err := statements(freshName, definer)
	if err != nil {
		t.Fatal(err)
	}
	for i := range freshReference {
		freshReference[i].Before = strings.ReplaceAll(reference[i].Before, f.database, freshName)
		freshReference[i].After = strings.ReplaceAll(reference[i].After, f.database, freshName)
	}
	freshPlan, err := bindPlan(freshName, definer, freshReference)
	if err != nil {
		t.Fatal(err)
	}
	freshRunner, err := migrations.New(freshDB, "decision_engine", freshPlan)
	if err != nil {
		t.Fatal(err)
	}
	if err := freshRunner.Apply(ctx); err != nil {
		t.Fatal("fresh foundation install", err)
	}
	if err := freshRunner.Apply(ctx); err != nil {
		t.Fatal("repeat fresh install", err)
	}
	if err := freshRunner.Verify(ctx, nil); err != nil {
		t.Fatal("fresh verification", err)
	}
	grants, err := definerGrants(f.database, definer)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if _, err := f.db.ExecContext(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	// Root installed the routines; the locked definer is not the installer.
	var locked string
	if err := f.db.QueryRowContext(ctx, "SELECT account_locked FROM mysql.user WHERE User=? AND Host='localhost'", definer).Scan(&locked); err != nil || locked != "Y" {
		t.Fatal("definer not locked", err)
	}
	return platformFixture{db: f.db, database: f.database, runner: runner, plan: plan, definer: definer}
}

func scenarioInsert(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, database string, tenant, id uuid.UUID, name string) error {
	schema, _ := mysqlstore.Identifier(database)
	now := mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}
	_, err := db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES(?,?,?,?,?,?)", mysqlstore.UUID(id), mysqlstore.UUID(tenant), mysqlstore.Text(name), mysqlstore.Text("wallets"), now, now)
	return err
}

func creator(t *testing.T, f platformFixture, tenant uuid.UUID) *IterationCreator {
	t.Helper()
	s, err := NewIterationCreator(f.db, f.database, tenant, allowPlatformFixture)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMySQLScenarioExactKeysAndRestrictedBoundary(t *testing.T) {
	f := scenarioFoundationDB(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	schema, _ := mysqlstore.Identifier(f.database)
	// Different exact spellings, including long values and SQL-looking names.
	names := []string{"Watch", "watch", "watch ", "é", "e\u0301", "", strings.Repeat("long", 3000), "x';DROP TABLE scenarios;--"}
	for _, name := range names {
		if err := scenarioInsert(ctx, f.db, f.database, tenant, uuid.New(), name); err != nil {
			t.Fatal(err)
		}
	}
	if err := scenarioInsert(ctx, f.db, f.database, tenant, uuid.New(), names[0]); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := scenarioInsert(ctx, f.db, f.database, other, uuid.New(), names[0]); err != nil {
		t.Fatal("tenant independence", err)
	}
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT name_key_id) FROM "+schema+".decision_engine_scenarios WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&count); err != nil || count != len(names) {
		t.Fatal("exact name collapse", count, err)
	}
	// A dedicated runtime account can mutate names through triggers but cannot
	// read/forge dictionary identities or call the resolver directly.
	runtime := "de_r_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	account := "`" + runtime + "`@`%`"
	if _, err := f.db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'disposable-runtime-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.ExecContext(ctx, "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	for _, g := range []string{"GRANT SELECT,INSERT,UPDATE ON " + schema + ".decision_engine_scenarios TO " + account} {
		if _, err := f.db.ExecContext(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.ExecContext(ctx, "GRANT INSERT ON "+schema+".decision_engine_scenario_versions TO "+account); err != nil {
		t.Fatal(err)
	}
	runtimeDB, err := mysqlstore.Open(ctx, platformConfig(f.database, runtime, "disposable-runtime-only"))
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	a, b := uuid.New(), uuid.New()
	if err := scenarioInsert(ctx, runtimeDB, f.database, tenant, a, "runtime"); err != nil {
		t.Fatal("restricted trigger execution", err)
	}
	if err := scenarioInsert(ctx, runtimeDB, f.database, tenant, b, "different"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeDB.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scenarios SET name_key_id=? WHERE id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(a)); err != nil {
		t.Fatal("trigger must overwrite forged key", err)
	}
	if _, err := runtimeDB.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scenarios SET name='runtime' WHERE id=?", mysqlstore.UUID(b)); err == nil {
		t.Fatal("duplicate rename accepted")
	}
	if _, err := runtimeDB.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scenarios SET tenant_id=? WHERE id=?", mysqlstore.UUID(other), mysqlstore.UUID(a)); err == nil {
		t.Fatal("ownership transfer accepted")
	}
	for _, q := range []string{"SELECT * FROM " + schema + ".decision_engine_exact_keys", "CALL " + schema + ".decision_engine_resolve_exact_key(_binary'de.scenario.name',X'00000000000000000000000000000001',_binary'raw',@key)", "DELETE FROM " + schema + ".decision_engine_exact_key_buckets", "DROP TRIGGER " + schema + ".decision_engine_scenario_name_insert"} {
		if _, err := runtimeDB.ExecContext(ctx, q); err == nil {
			t.Fatal("restricted bypass succeeded", q)
		}
	}
	conn, err := runtimeDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"unique_checks", "foreign_key_checks"} {
		if _, err := conn.ExecContext(ctx, "SET SESSION "+flag+"=0"); err != nil {
			t.Fatal(err)
		}
		insertErr := scenarioInsert(ctx, conn, f.database, tenant, uuid.New(), "disabled-checks")
		_, counterErr := conn.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_scenario_versions(tenant_id,scenario_id) VALUES(?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.New()))
		if _, err := conn.ExecContext(ctx, "SET SESSION "+flag+"=1"); err != nil {
			t.Fatal(err)
		}
		if insertErr == nil || counterErr == nil {
			t.Fatal("disabled integrity checks accepted", flag)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	// Force collisions only in this generated fixture; no production hash edit.
	if _, err := f.db.ExecContext(ctx, "DROP FUNCTION "+schema+".decision_engine_exact_key_digest"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "CREATE FUNCTION "+schema+".decision_engine_exact_key_digest(p LONGBLOB) RETURNS BINARY(32) DETERMINISTIC NO SQL RETURN UNHEX(REPEAT('00',32))"); err != nil {
		t.Fatal(err)
	}
	// DROP FUNCTION removed its specific EXECUTE grant. Restore that fixture
	// privilege so the collision test exercises equality, not missing grants.
	var definer string
	if err := f.db.QueryRowContext(ctx, "SELECT DEFINER FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=? AND ROUTINE_NAME='decision_engine_resolve_exact_key'", f.database).Scan(&definer); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(definer, "@")
	if len(parts) != 2 || parts[1] != "localhost" {
		t.Fatal("unexpected fixture definer")
	}
	user, err := mysqlstore.Identifier(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION "+schema+".decision_engine_exact_key_digest TO "+user+"@`localhost`"); err != nil {
		t.Fatal(err)
	}
	if err := f.runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrDrift) {
		t.Fatal("digest drift not rejected", err)
	}
	collisionTenant := uuid.New()
	for _, name := range []string{"collision-a", "collision-b"} {
		if err := scenarioInsert(ctx, f.db, f.database, collisionTenant, uuid.New(), name); err != nil {
			t.Fatal(err)
		}
	}
	if err := scenarioInsert(ctx, f.db, f.database, collisionTenant, uuid.New(), "collision-a"); err == nil {
		t.Fatal("collision duplicate accepted")
	}
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT name_key_id) FROM "+schema+".decision_engine_scenarios WHERE tenant_id=?", mysqlstore.UUID(collisionTenant)).Scan(&count); err != nil || count != 2 {
		t.Fatal("collision changed identity", count, err)
	}
}

func TestMySQLScenarioTenantRelationshipsAndDeletion(t *testing.T) {
	f := scenarioFoundationDB(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	tenant, other, parent, otherParent, otherTenantParent := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, pair := range [][2]uuid.UUID{{tenant, parent}, {tenant, otherParent}, {other, otherTenantParent}} {
		if err := scenarioInsert(ctx, f.db, f.database, pair[0], pair[1], uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	input := DraftInput{ID: uuid.New(), ScenarioID: parent, TriggerFormula: json.RawMessage(`{"constant":true}`)}
	result, err := creator(t, f, tenant).CreateDraft(ctx, uuid.New(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Iteration.Version != 1 {
		t.Fatal("first version")
	}
	iteration := mysqlstore.UUID(input.ID)
	a, p := mysqlstore.UUID(tenant), mysqlstore.UUID(parent)
	for _, binding := range [][2]uuid.UUID{{tenant, otherParent}, {other, parent}, {other, otherTenantParent}} {
		if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_scenario_live_iterations(tenant_id,scenario_id,iteration_id) VALUES(?,?,?)", mysqlstore.UUID(binding[0]), mysqlstore.UUID(binding[1]), iteration); err == nil {
			t.Fatal("cross-scope live link accepted")
		}
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_scenario_live_iterations VALUES(?,?,?)", a, p, iteration); err != nil {
		t.Fatal(err)
	}
	now := mysqlstore.DateTime{Time: time.Now().UTC().Truncate(time.Microsecond)}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_rules(id,tenant_id,iteration_id,name,created_at,updated_at) VALUES(?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(other), iteration, "wrong", now, now); err == nil {
		t.Fatal("cross-tenant rule accepted")
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_scenario_publications(id,tenant_id,scenario_id,iteration_id,action,created_at) VALUES(?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), a, mysqlstore.UUID(otherParent), iteration, "publish", now); err == nil {
		t.Fatal("cross-scenario publication accepted")
	}
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_rules(id,tenant_id,iteration_id,name,created_at,updated_at) VALUES(?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), a, iteration, "valid", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "DELETE FROM "+schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND id=?", a, iteration); err != nil {
		t.Fatal(err)
	}
	var live mysqlstore.NullUUID
	if err := f.db.QueryRowContext(ctx, "SELECT l.iteration_id FROM "+schema+".decision_engine_scenarios s LEFT JOIN "+schema+".decision_engine_scenario_live_iterations l ON l.tenant_id=s.tenant_id AND l.scenario_id=s.id WHERE s.tenant_id=? AND s.id=?", a, p).Scan(&live); err != nil || live.Valid {
		t.Fatal("scenario did not survive with absent link", err)
	}
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_rules WHERE tenant_id=?", a).Scan(&count); err != nil || count != 0 {
		t.Fatal("rule cascade", count, err)
	}
	next := input
	next.ID = uuid.New()
	r, err := creator(t, f, tenant).CreateDraft(ctx, uuid.New(), next)
	if err != nil || r.Iteration.Version != 2 {
		t.Fatal("deleted version reused", r, err)
	}
}

func TestMySQLIterationCreationReplayConcurrencyAndRollback(t *testing.T) {
	f := scenarioFoundationDB(t)
	ctx := context.Background()
	tenant, parent := uuid.New(), uuid.New()
	schema, _ := mysqlstore.Identifier(f.database)
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "versions"); err != nil {
		t.Fatal(err)
	}
	s := creator(t, f, tenant)
	operation := uuid.New()
	input := DraftInput{ID: uuid.New(), ScenarioID: parent, TriggerFormula: json.RawMessage(`{"constant":true}`)}
	first, err := s.CreateDraft(ctx, operation, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateDraft(ctx, operation, input)
	if err != nil || !replay.Replay || !reflect.DeepEqual(first.Iteration, replay.Iteration) {
		t.Fatal("receipt replay differs", err)
	}
	changed := input
	changed.Schedule = "changed"
	if _, err := s.CreateDraft(ctx, operation, changed); !errors.Is(err, ErrIterationReplayConflict) {
		t.Fatal("conflicting operation", err)
	}
	if _, err := s.CreateDraft(ctx, uuid.New(), input); err == nil {
		t.Fatal("different operation reused an existing iteration ID")
	}
	for _, q := range []string{
		"UPDATE " + schema + ".decision_engine_iteration_creations SET result_body=result_body WHERE tenant_id=?",
		"DELETE FROM " + schema + ".decision_engine_iteration_creations WHERE tenant_id=?",
		"UPDATE " + schema + ".decision_engine_scenario_iterations SET version=99 WHERE tenant_id=?",
	} {
		if _, err := f.db.ExecContext(ctx, q, mysqlstore.UUID(tenant)); err == nil {
			t.Fatal("immutable identity/receipt changed", q)
		}
	}
	// A later receipt INSERT failure must undo both iteration and counter.
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_fail_receipt BEFORE INSERT ON "+schema+".decision_engine_iteration_creations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture receipt failure'"); err != nil {
		t.Fatal(err)
	}
	failed := input
	failed.ID = uuid.New()
	if _, err := s.CreateDraft(ctx, uuid.New(), failed); err == nil {
		t.Fatal("receipt failure hidden")
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_fail_receipt"); err != nil {
		t.Fatal(err)
	}
	var count, version int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(failed.ID)).Scan(&count); err != nil || count != 0 {
		t.Fatal("iteration leaked", count, err)
	}
	if err := f.db.QueryRowContext(ctx, "SELECT last_version FROM "+schema+".decision_engine_scenario_versions WHERE tenant_id=? AND scenario_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(parent)).Scan(&version); err != nil || version != 1 {
		t.Fatal("counter leaked", version, err)
	}
	// Eight same-scenario operations receive consecutive distinct versions.
	var wg sync.WaitGroup
	results := make(chan IterationCreation, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := input
			v.ID = uuid.New()
			r, err := s.CreateDraft(ctx, uuid.New(), v)
			if err != nil {
				failures <- err
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	versions := map[int]bool{}
	for r := range results {
		versions[r.Iteration.Version] = true
	}
	for n := 2; n <= 9; n++ {
		if !versions[n] {
			t.Fatal("missing concurrent version", n)
		}
	}
	// Same-key concurrent callers replay a single committed result.
	sameOp := uuid.New()
	same := input
	same.ID = uuid.New()
	results = make(chan IterationCreation, 4)
	failures = make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.CreateDraft(ctx, sameOp, same)
			if err != nil {
				failures <- err
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	fresh := 0
	for r := range results {
		if !r.Replay {
			fresh++
		}
		if r.Iteration.Version != 10 {
			t.Fatal("same op allocated again")
		}
	}
	if fresh != 1 {
		t.Fatal("same op winners", fresh)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_scenario_versions SET last_version=? WHERE tenant_id=? AND scenario_id=?", math.MaxInt32, mysqlstore.UUID(tenant), mysqlstore.UUID(parent)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDraft(ctx, uuid.New(), failed); !errors.Is(err, ErrIterationVersionExhausted) {
		t.Fatal("overflow", err)
	}
	if r, err := s.CreateDraft(ctx, operation, input); err != nil || !r.Replay {
		t.Fatal("overflow blocked existing replay", err)
	}
	// Wrong tenant cannot use another tenant's parent/receipt, even with same op.
	wrong := creator(t, f, uuid.New())
	if _, err := wrong.CreateDraft(ctx, operation, input); err == nil {
		t.Fatal("cross-tenant create/replay")
	}
	denied := errors.New("fixture denied")
	guarded, err := NewIterationCreator(f.db, f.database, tenant, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return denied })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guarded.CreateDraft(ctx, operation, input); !errors.Is(err, denied) {
		t.Fatal("replay bypassed authorization", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := s.CreateDraft(canceled, uuid.New(), failed); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestMySQLIterationCounterDoesNotSerializeOtherScenario(t *testing.T) {
	f := scenarioFoundationDB(t)
	ctx := context.Background()
	tenant, p1, p2 := uuid.New(), uuid.New(), uuid.New()
	schema, _ := mysqlstore.Identifier(f.database)
	for _, id := range []uuid.UUID{p1, p2} {
		if err := scenarioInsert(ctx, f.db, f.database, tenant, id, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	s := creator(t, f, tenant)
	for _, id := range []uuid.UUID{p1, p2} {
		if _, err := s.CreateDraft(ctx, uuid.New(), DraftInput{ID: uuid.New(), ScenarioID: id, TriggerFormula: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var v int
	if err := tx.QueryRowContext(ctx, "SELECT last_version FROM "+schema+".decision_engine_scenario_versions WHERE tenant_id=? AND scenario_id=? FOR UPDATE", mysqlstore.UUID(tenant), mysqlstore.UUID(p1)).Scan(&v); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	r, err := s.CreateDraft(deadline, uuid.New(), DraftInput{ID: uuid.New(), ScenarioID: p2, TriggerFormula: json.RawMessage(`{}`)})
	if err != nil || r.Iteration.Version != 2 {
		t.Fatal("unrelated scenario blocked", r, err)
	}
}

func TestMySQLScenarioNameAllocationRollbackAndConcurrency(t *testing.T) {
	f := scenarioFoundationDB(t)
	ctx := context.Background()
	tenant := uuid.New()
	schema, _ := mysqlstore.Identifier(f.database)
	rollback := errors.New("fixture abort")
	if err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		if err := scenarioInsert(ctx, tx, f.database, tenant, uuid.New(), "rollback"); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_exact_keys WHERE scope_bytes=?", mysqlstore.UUID(tenant)).Scan(&count); err != nil || count != 0 {
		t.Fatal("dictionary allocation survived rollback", count, err)
	}
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out <- scenarioInsert(ctx, f.db, f.database, tenant, uuid.New(), "concurrent")
		}()
	}
	wg.Wait()
	close(out)
	successes, failures := 0, 0
	for err := range out {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatal(fmt.Sprintf("name winners=%d conflicts=%d", successes, failures))
	}
}
