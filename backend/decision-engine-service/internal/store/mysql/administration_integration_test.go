package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func adminFixture(t *testing.T) (effectFixture, *AdministrationStore) {
	t.Helper()
	f := effectsFixture(t)
	s, e := NewAdministrationStore(f.db, f.database, f.tenant, 500, 2<<20, allowAuthoringFixture, nil, serviceIDsFixture{}, serviceClockFixture{time.Now().UTC().Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	return f, s
}
func adminWorkflow() workflow.Definition {
	return workflow.Definition{Name: "event", AllowedOutcomes: []string{"review", "review"}, ActionType: workflow.ActionTypeEmitEvent, ActionConfig: []byte(`{"url":"https://example.test/event"}`), Active: true}
}

func TestMySQLAdministrationStaleWorkflowWriteRejected(t *testing.T) {
	f, s := adminFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(f.database)
	saved, e := s.SaveWorkflow(ctx, f.parent, uuid.Nil, adminWorkflow())
	if e != nil {
		t.Fatal(e)
	}
	lock, e := f.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback()
	var owner mysqlstore.UUID
	if e = lock.QueryRowContext(ctx, "SELECT scenario_id FROM "+schema+".decision_engine_workflow_authoring_guards WHERE tenant_id=? AND scenario_id=? FOR UPDATE", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.parent)).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		v := adminWorkflow()
		v.Name = "stale replacement"
		_, e := s.SaveWorkflow(ctx, f.parent, uuid.MustParse(saved.ID), v)
		result <- e
	}()
	// Observe the actual SQL wait, rather than assuming a sleep ordered the reads.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		e = f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='decision_engine_workflow_authoring_guards'", f.database).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer never reached guard")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e = lock.ExecContext(ctx, "UPDATE "+schema+".decision_engine_workflows SET name='newer configuration' WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(saved.ID))); e != nil {
		t.Fatal(e)
	}
	if e = lock.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-result; !errors.Is(e, ErrOwnerRelationshipConflict) {
		t.Fatal("stale write accepted", e)
	}
	current, e := s.Workflow(ctx, f.parent, uuid.MustParse(saved.ID))
	if e != nil || current.Name != "newer configuration" {
		t.Fatal(current, e)
	}
}

func TestMySQLAdministrationAppendBeyondListCap(t *testing.T) {
	f, s := adminFixture(t)
	ctx := context.Background()
	// A small configured list cap must not turn allocation into a partial-history
	// scan. Existing rows deliberately exceed this cap.
	small, e := NewAdministrationStore(f.db, f.database, f.tenant, 2, 2<<20, allowAuthoringFixture, nil, serviceIDsFixture{}, s.clock)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 32; i++ {
		v, e := small.SaveWorkflow(ctx, f.parent, uuid.Nil, adminWorkflow())
		if e != nil || v.DisplayOrder != i+1 {
			t.Fatal("append beyond cap", v.DisplayOrder, e)
		}
	}
	if _, e = small.Workflows(ctx, f.parent); !errors.Is(e, ErrProjectionLimit) {
		t.Fatal("list silently truncated", e)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	var plan string
	if e = f.db.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON SELECT MAX(display_order) FROM "+schema+".decision_engine_workflows WHERE tenant_id=? AND scenario_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.parent)).Scan(&plan); e != nil {
		t.Fatal(e)
	}
	t.Logf("33-row allocation correctness fixture; not load acceptance: %s", plan)
}

func TestMySQLAdministrationWorkflowConcurrencyOrderingAndRollback(t *testing.T) {
	f, s := adminFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	const n = 12
	results := make(chan workflow.Definition, n)
	fail := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.SaveWorkflow(ctx, f.parent, uuid.Nil, adminWorkflow())
			results <- v
			fail <- e
		}()
	}
	wg.Wait()
	close(results)
	close(fail)
	for e := range fail {
		if e != nil {
			t.Fatal(e)
		}
	}
	seen := map[int]bool{}
	for v := range results {
		if seen[v.DisplayOrder] || v.DisplayOrder < 1 {
			t.Fatal("duplicate order", v)
		}
		seen[v.DisplayOrder] = true
	}
	before, e := s.Workflows(ctx, f.parent)
	if e != nil {
		t.Fatal(e)
	}
	ids := make([]string, len(before))
	for i, v := range before {
		ids[len(ids)-1-i] = v.ID
	}
	if e = s.ReorderWorkflows(ctx, f.parent, ids); e != nil {
		t.Fatal(e)
	}
	after, e := s.Workflows(ctx, f.parent)
	if e != nil {
		t.Fatal(e)
	}
	for i, v := range after {
		if v.ID != ids[i] || v.DisplayOrder != i {
			t.Fatal("order", after)
		}
	}
	if e = s.ReorderWorkflows(ctx, f.parent, ids[:1]); !errors.Is(e, ErrAdministrationInput) {
		t.Fatal("partial reorder", e)
	}
	if _, e = f.db.Exec("CREATE TRIGGER " + schema + ".fixture_admin_reorder BEFORE UPDATE ON " + schema + ".decision_engine_workflows FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fault'"); e != nil {
		t.Fatal(e)
	}
	if e = s.ReorderWorkflows(ctx, f.parent, ids); e == nil {
		t.Fatal("reorder fault ignored")
	}
	if _, e = f.db.Exec("DROP TRIGGER " + schema + ".fixture_admin_reorder"); e != nil {
		t.Fatal(e)
	}
	restored, e := s.Workflows(ctx, f.parent)
	if e != nil || !reflect.DeepEqual(after, restored) {
		t.Fatal("rollback", e)
	}
	// An unrelated scenario is not serialized by the selected workflow guard.
	other := uuid.New()
	if e := scenarioInsert(ctx, f.db, f.database, f.tenant, other, "Independent"); e != nil {
		t.Fatal(e)
	}
	lock, e := f.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback()
	if _, e = lock.ExecContext(ctx, "SELECT scenario_id FROM "+schema+".decision_engine_workflow_authoring_guards WHERE tenant_id=? AND scenario_id=? FOR UPDATE", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.parent)); e != nil {
		t.Fatal(e)
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, e = s.SaveWorkflow(bounded, other, uuid.Nil, adminWorkflow()); e != nil {
		t.Fatal("unrelated workflow blocked", e)
	}
}

func TestMySQLAdministrationTestRunSnoozeAuthorizationAndRollback(t *testing.T) {
	f, s := adminFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	a := authoringStore(t, f.platformFixture, f.tenant, 500, 2<<20)
	phantom := createAuthoringDraft(t, a, f.tenant, f.parent)
	at := s.now()
	if e := a.Mutate(ctx, func(r AuthoringRepositories) error {
		_, e := r.Iterations.Commit(ctx, f.tenant.String(), f.parent.String(), f.iteration.ID, at)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Publish(ctx, PublicationInput{f.parent, uuid.MustParse(f.iteration.ID), uuid.New(), uuid.New()}, func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateTestRun(ctx, f.parent, uuid.MustParse(f.iteration.ID), at.Add(time.Hour)); !errors.Is(e, ErrAdministrationInput) {
		t.Fatal("same iteration", e)
	}
	if _, e := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_admin_run BEFORE INSERT ON " + schema + ".decision_engine_test_runs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fault'"); e != nil {
		t.Fatal(e)
	}
	failed, e := s.CreateTestRun(ctx, f.parent, uuid.MustParse(phantom.ID), at.Add(time.Hour))
	if e == nil || failed.ID != "" {
		t.Fatal("provisional result", failed, e)
	}
	if _, e = f.db.Exec("DROP TRIGGER " + schema + ".fixture_admin_run"); e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateTestRun(ctx, f.parent, uuid.MustParse(phantom.ID), at.Add(time.Hour))
	if e != nil || run.LiveIterationID != f.iteration.ID || run.Status != scenario.TestRunStatusUp {
		t.Fatal(run, e)
	}
	if _, e = f.db.Exec("CREATE TRIGGER " + schema + ".fixture_admin_cancel BEFORE UPDATE ON " + schema + ".decision_engine_test_runs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fault'"); e != nil {
		t.Fatal(e)
	}
	failed, e = s.CancelTestRun(ctx, uuid.MustParse(run.ID))
	if e == nil || failed.ID != "" {
		t.Fatal("cancel fault", failed, e)
	}
	if _, e = f.db.Exec("DROP TRIGGER " + schema + ".fixture_admin_cancel"); e != nil {
		t.Fatal(e)
	}
	reader, readErr := NewDecisionStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if e := reader.ReadHistory(ctx, func(r HistoryRepositories) error {
		v, e := r.TestRuns.GetByID(ctx, f.tenant.String(), run.ID)
		if e == nil && v.Status != scenario.TestRunStatusUp {
			t.Fatal("cancel not rolled back")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	// History reads use a separate explicitly authorized store.
	input := snooze.RuleSnooze{ObjectType: "wallets", ObjectID: "Wallet ", SnoozeGroupID: uuid.NewString(), ExpiresAt: at.Add(time.Hour)}
	denied, e := NewAdministrationStore(f.db, f.database, f.tenant, 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, AuthoringAction) error {
		return ErrDecisionAuthorizationDenied
	}, nil, serviceIDsFixture{}, serviceClockFixture{at})
	if e != nil {
		t.Fatal(e)
	}
	if v, e := denied.CreateSnooze(ctx, f.parent, input); !errors.Is(e, ErrDecisionAuthorizationDenied) || v.ID != "" {
		t.Fatal("denied snooze", v, e)
	}
	if _, e = f.db.Exec("CREATE TRIGGER " + schema + ".fixture_admin_snooze BEFORE INSERT ON " + schema + ".decision_engine_rule_snoozes FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fault'"); e != nil {
		t.Fatal(e)
	}
	if v, e := s.CreateSnooze(ctx, f.parent, input); e == nil || v.ID != "" {
		t.Fatal("snooze rollback", v, e)
	}
	if _, e = f.db.Exec("DROP TRIGGER " + schema + ".fixture_admin_snooze"); e != nil {
		t.Fatal(e)
	}
	if v, e := s.CreateSnooze(ctx, f.parent, input); e != nil || v.ObjectID != "Wallet " {
		t.Fatal(v, e)
	}
}

func TestMySQLAdministrationCaseDestinationAndWorkflowAuthority(t *testing.T) {
	f, _ := adminFixture(t)
	ctx := context.Background()
	inbox, tag := uuid.New(), uuid.New()
	session := TrustedDecisionSession{ActorID: uuid.New(), TenantID: f.tenant, SessionID: uuid.New(), MembershipVersion: 3}
	browserProviderFixtures(t, f.platformFixture, session, inbox, tag)
	auth, e := SessionAuthorizer(f.database, session)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewAdministrationStore(f.db, f.database, f.tenant, 500, 2<<20, auth, &session, serviceIDsFixture{}, serviceClockFixture{time.Now().UTC().Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	v := adminWorkflow()
	v.ActionType = workflow.ActionTypeCreateCase
	v.ActionConfig = []byte(fmt.Sprintf(`{"inbox_id":%q,"tag_ids":[%q]}`, inbox.String(), tag.String()))
	saved, e := s.SaveWorkflow(ctx, f.parent, uuid.Nil, v)
	if e != nil {
		t.Fatal(e)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, e = f.db.Exec("UPDATE "+schema+".fixture_browser_permission SET active=0 WHERE id=?", mysqlstore.UUID(inbox)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveWorkflow(ctx, f.parent, uuid.MustParse(saved.ID), v); !errors.Is(e, ErrDecisionAuthorizationDenied) {
		t.Fatal("inactive inbox allowed", e)
	}
	v.Active = false
	if _, e = s.SaveWorkflow(ctx, f.parent, uuid.MustParse(saved.ID), v); e != nil {
		t.Fatal("cannot disable inactive destination", e)
	}
	if _, e = f.db.Exec("UPDATE "+schema+".fixture_browser_permission SET allowed=0 WHERE id=?", mysqlstore.UUID(inbox)); e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteWorkflow(ctx, f.parent, uuid.MustParse(saved.ID)); !errors.Is(e, ErrDecisionAuthorizationDenied) {
		t.Fatal("delete bypassed inbox", e)
	}
	if _, e = s.Workflows(ctx, f.parent); !errors.Is(e, ErrDecisionAuthorizationDenied) {
		t.Fatal("list bypassed inbox", e)
	}
	if _, e = f.db.Exec("UPDATE " + schema + ".fixture_browser_permission SET allowed=1,active=1"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec("DROP FUNCTION " + schema + ".case_manager_authorize_decision_tags_v1"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveWorkflow(ctx, f.parent, uuid.MustParse(saved.ID), v); !errors.Is(e, ErrOwnerContractUnavailable) {
		t.Fatal("missing tag provider allowed", e)
	}
	if _, e = s.CreateSnooze(ctx, f.parent, snooze.RuleSnooze{}); !errors.Is(e, ErrDecisionAuthorizationDenied) {
		t.Fatal("workflow-only authority leaked", e)
	}
}
