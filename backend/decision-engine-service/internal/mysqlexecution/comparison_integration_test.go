package mysqlexecution

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
	"testing"
	"time"
)

func comparisonFixture(t *testing.T) (*preparationFixture, *mysql.AdministrationStore, *Direct, scenario.TestRun) {
	t.Helper()
	f := setupPreparation(t)
	ctx := context.Background()
	draft, err := f.authoring.CloneIteration(ctx, f.parent, f.old, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := mysql.NewAdministrationStore(f.db, f.database, f.tenant, 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, mysql.AuthoringAction) error {
		if f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	}, nil, ids{}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := admin.CreateTestRun(ctx, f.parent, uuid.MustParse(draft.ID), clock{}.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	direct, err := NewDirect(f.preparer, f.unit)
	if err != nil {
		t.Fatal(err)
	}
	return f, admin, direct, run
}
func TestMySQLComparisonDraftPersistenceAnalyticsAndCancellationFence(t *testing.T) {
	f, admin, direct, run := comparisonFixture(t)
	ctx := context.Background()
	result, err := direct.Compare(ctx, admin, uuid.MustParse(run.ID), workerItems()[0])
	if err != nil {
		t.Fatal(err)
	}
	if result.Live.Decision.Score != 15 || result.Phantom.Decision.Score != 15 {
		t.Fatal(result)
	}
	if f.count(t, "decision_engine_decisions") != 0 || f.count(t, "decision_engine_phantom_decisions") != 1 || f.count(t, "decision_engine_phantom_rule_executions") != 1 {
		t.Fatal("comparison persisted the wrong owners")
	}
	scores, err := admin.ComparisonScores(ctx, uuid.MustParse(run.ID))
	if err != nil || len(scores) != 1 || scores[0].Count != 1 {
		t.Fatal(scores, err)
	}
	rules, err := admin.ComparisonRules(ctx, uuid.MustParse(run.ID))
	if err != nil || len(rules) != 1 || rules[0].HitCount != 1 {
		t.Fatal(rules, err)
	}
	f.provider.mu.Lock()
	f.provider.onRead = func() {
		if _, err := admin.CancelTestRun(ctx, uuid.MustParse(run.ID)); err != nil {
			t.Error(err)
		}
	}
	f.provider.mu.Unlock()
	if _, err := direct.Compare(ctx, admin, uuid.MustParse(run.ID), workerItems()[0]); !errors.Is(err, mysql.ErrExecutionBindingChanged) {
		t.Fatal("in-flight cancellation was not fenced", err)
	}
	if f.count(t, "decision_engine_phantom_decisions") != 1 {
		t.Fatal("cancelled comparison wrote evidence")
	}
}
func TestMySQLComparisonLateEvidenceFailureRollsBack(t *testing.T) {
	f, admin, direct, run := comparisonFixture(t)
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".fixture_comparison_failure BEFORE INSERT ON " + f.schema + ".decision_engine_phantom_rule_executions FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated comparison rollback'"); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.Compare(context.Background(), admin, uuid.MustParse(run.ID), workerItems()[0]); err == nil {
		t.Fatal("late comparison failure accepted")
	}
	for _, table := range []string{"decision_engine_decisions", "decision_engine_phantom_decisions", "decision_engine_phantom_rule_executions"} {
		if f.count(t, table) != 0 {
			t.Fatal("partial comparison persisted", table)
		}
	}
}
