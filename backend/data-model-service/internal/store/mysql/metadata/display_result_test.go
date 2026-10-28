package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Observe only in the generated disposable namespace, then install the source-bound upgrade.
func installDisplayResultFixture(t *testing.T, ctx context.Context, db *sql.DB, database string, installed migrations.Plan) (migrations.Plan, installation.Definer) {
	t.Helper()
	guard := installation.Definer{User: "or_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}
	account, _ := guard.SQLAccount()
	create, err := installation.CreateDefiner(guard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, create); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	steps, err := installation.OwnerResultUpgradeStatements(database, guard)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range steps {
		steps[i].Before, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(err)
		}
		steps[i].After, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := installation.ExtendOwnerResultPlan(installed, guard, steps)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"sql", "name", "kind", "id", "before", "missing", "order"} {
		bad := append([]migrations.Step(nil), steps...)
		switch change {
		case "sql":
			bad[0].SQL += " "
		case "name":
			bad[0].Name = "substituted"
		case "kind":
			bad[0].Kind = "TRIGGER"
		case "id":
			bad[0].ID = "substituted"
		case "before":
			bad[0].Before = bad[0].After
		case "missing":
			bad = bad[:3]
		case "order":
			bad[1], bad[2] = bad[2], bad[1]
		}
		if _, e := installation.ExtendOwnerResultPlan(installed, guard, bad); e == nil {
			t.Fatal("accepted invalid owner result binding", change)
		}
	}
	if len(plan.Steps) != 89 {
		t.Fatal("incomplete v004", len(plan.Steps))
	}
	if !strings.HasPrefix(database, "data_model_metadata_test_") {
		t.Fatal("unsafe fixture namespace")
	}
	schema, _ := mysqlstore.Identifier(database)
	if _, err = db.ExecContext(ctx, "DROP TABLE "+schema+".data_model_owner_results"); err != nil {
		t.Fatal(err)
	}
	partial, err := migrations.New(db, migrations.Plan{Database: database, Steps: plan.Steps[:86]})
	if err != nil {
		t.Fatal(err)
	}
	if err = partial.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.New(db, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(ctx); err != nil {
		t.Fatal("v004 replay", err)
	}
	policy := installation.OwnerResultDefinerPolicy(guard)
	grants, err := installation.GrantStatements(database, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	conn, err = db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = installation.VerifyAccountPolicy(ctx, conn, database, policy); err != nil {
		t.Fatal(err)
	}
	return plan, guard
}

func verifyDisplayResult(t *testing.T, ctx context.Context, db *sql.DB, database string, s *store.DisplayStore, p *store.PublicationStore, tenant, table, actor uuid.UUID, initial contract.Publication, input display.SaveRequest, scenario string) {
	t.Helper()
	operation := uuid.New()
	condition := contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: initial.Version}
	var calls atomic.Int32
	var revoked atomic.Bool
	denied := errors.New("revoked display authorization")
	auth := func(_ context.Context, scope, target, user uuid.UUID) error {
		if revoked.Load() || scope != tenant || target != table || user != actor {
			return denied
		}
		return nil
	}
	effects := func(context.Context, *sql.Tx) error {
		calls.Add(1)
		if scenario == "rollback" {
			return denied
		}
		return nil
	}
	before, err := s.Get(ctx, table)
	if err != nil {
		t.Fatal(err)
	}
	var result display.Settings
	if scenario == "legacy" {
		if _, err = s.SavePublished(ctx, table, actor, operation, condition, input, p, auth, effects); err != nil {
			t.Fatal(err)
		}
		_, err = s.SavePublishedSettings(ctx, table, actor, operation, condition, input, p, auth, effects)
		if !errors.Is(err, store.ErrOwnerResultUnavailable) || calls.Load() != 1 {
			t.Fatal("legacy reconstruction", err, calls.Load())
		}
		return
	}
	activeS, activeP := s, p
	var observed atomic.Bool
	if scenario == "lost_commit" {
		fault := deliveryFaultDB(t, database, &observed)
		activeS, err = store.NewDisplayStore(fault, database, tenant, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		activeP, err = store.NewPublicationStore(fault, database, 100, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
	}
	save := func() (display.Settings, error) {
		return activeS.SavePublishedSettings(ctx, table, actor, operation, condition, input, activeP, auth, effects)
	}
	if scenario == "concurrent" {
		var wg sync.WaitGroup
		results := make(chan display.Settings, 8)
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); v, e := save(); results <- v; errs <- e }()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		for v := range results {
			if result.TableID != uuid.Nil && !reflect.DeepEqual(result, v) {
				t.Fatal("different concurrent results")
			}
			result = v
		}
	} else {
		result, err = save()
	}
	if scenario == "rollback" {
		if !errors.Is(err, denied) || result.TableID != uuid.Nil {
			t.Fatal("owner rollback", err)
		}
		after, e := s.Get(ctx, table)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("rollback changed settings", e)
		}
		for _, relation := range []string{"data_model_publication_operations", "data_model_owner_results"} {
			var n int
			if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+relation+" WHERE tenant_id=? AND table_id=? AND operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(operation)).Scan(&n); e != nil || n != 0 {
				t.Fatal("rollback receipt", relation, e, n)
			}
		}
		return
	}
	if scenario == "lost_commit" {
		if err == nil || !observed.Load() {
			t.Fatal("expected lost commit", err)
		}
		result, err = s.SavePublishedSettings(ctx, table, actor, operation, condition, input, p, auth, effects)
	}
	if err != nil || result.Caption != input.Caption || result.Version <= before.Version || result.TenantID != tenant {
		t.Fatal("saved result", result, err)
	}
	current, readErr := s.Get(ctx, table)
	if readErr != nil || !reflect.DeepEqual(result, current) {
		t.Fatal("result differs from committed settings", readErr)
	}
	if calls.Load() != 1 {
		t.Fatal("repeated effects", calls.Load())
	}
	for _, q := range []string{"UPDATE data_model_owner_results SET result_kind='forged' WHERE operation_id=?", "UPDATE data_model_owner_results SET original_result=JSON_OBJECT() WHERE operation_id=?", "DELETE FROM data_model_owner_results WHERE operation_id=?"} {
		if _, e := db.ExecContext(ctx, q, mysqlstore.UUID(operation)); e == nil {
			t.Fatal("mutable receipt", q)
		}
	}
	small, e := store.NewDisplayStore(db, database, tenant, 100, 256)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = small.SavePublishedSettings(ctx, table, actor, operation, condition, input, p, auth, effects); !errors.Is(e, store.ErrSnapshotLimit) {
		t.Fatal("unbounded historical result", e)
	}
	if _, e = db.ExecContext(ctx, "INSERT INTO data_model_owner_results(tenant_id,table_id,operation_id,result_kind,original_result,created_at) VALUES (?,?,?,'display.settings/v1',JSON_OBJECT(),UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(table), mysqlstore.UUID(operation)); e == nil {
		t.Fatal("foreign owner result inserted")
	}
	conn, e := db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.ExecContext(ctx, "SET SESSION foreign_key_checks=0"); e != nil {
		t.Fatal(e)
	}
	_, disabledErr := conn.ExecContext(ctx, "UPDATE data_model_owner_results SET result_kind=result_kind WHERE operation_id=?", mysqlstore.UUID(operation))
	_, restoreErr := conn.ExecContext(ctx, "SET SESSION foreign_key_checks=1")
	closeErr := conn.Close()
	if disabledErr == nil || restoreErr != nil || closeErr != nil {
		t.Fatal("integrity-check bypass", disabledErr, restoreErr, closeErr)
	}
	next := display.SaveRequest{Version: &result.Version, Caption: ""}
	if _, e := s.SavePublishedSettings(ctx, table, actor, uuid.New(), condition, next, p, auth, func(context.Context, *sql.Tx) error { return nil }); e != nil {
		t.Fatal("later save", e)
	}
	if _, e := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)); e != nil {
		t.Fatal(e)
	}
	replay, e := s.SavePublishedSettings(ctx, table, actor, operation, condition, input, p, auth, effects)
	if e != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("historical settings changed", e, result, replay)
	}
	changed := input
	changed.Caption = ""
	if _, e = s.SavePublishedSettings(ctx, table, actor, operation, condition, changed, p, auth, effects); !errors.Is(e, store.ErrPublicationOperationConflict) {
		t.Fatal("changed replay", e)
	}
	revoked.Store(true)
	if _, e = s.SavePublishedSettings(ctx, table, actor, operation, condition, input, p, auth, effects); !errors.Is(e, denied) {
		t.Fatal("unauthorized replay", e)
	}
}

func verifyOwnerResultReadinessFixture(t *testing.T, ctx context.Context, db *sql.DB, database string, shared *installation.ExactKeyReadiness, guard installation.Definer) {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	verify := func() error {
		return shared.VerifyOnConn(ctx, conn, database, installation.Definer{User: "record_guard_test", Host: "%"})
	}
	if err = verify(); err != nil {
		t.Fatal("v004 readiness", err)
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := guard.SQLAccount()
	run := func(q string) {
		t.Helper()
		if _, e := conn.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	run("REVOKE SELECT ON " + schema + ".data_model_owner_results FROM " + account)
	if err = verify(); err == nil {
		t.Fatal("missing result definer grant accepted")
	}
	run("GRANT SELECT ON " + schema + ".data_model_owner_results TO " + account)
	steps, err := installation.OwnerResultUpgradeStatements(database, guard)
	if err != nil {
		t.Fatal(err)
	}
	run("DROP TRIGGER " + schema + ".data_model_owner_results_update_guard")
	if err = verify(); err == nil {
		t.Fatal("missing immutable guard accepted")
	}
	run(steps[2].SQL)
	run("CREATE TRIGGER " + schema + ".extra_result_trigger BEFORE INSERT ON " + schema + ".data_model_owner_results FOR EACH ROW SET @unused_result=1")
	if err = verify(); err == nil {
		t.Fatal("extra result trigger accepted")
	}
	run("DROP TRIGGER " + schema + ".extra_result_trigger")
	if err = verify(); err != nil {
		t.Fatal("restored result readiness", err)
	}
}
