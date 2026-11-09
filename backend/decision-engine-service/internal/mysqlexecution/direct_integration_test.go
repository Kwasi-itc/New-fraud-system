package mysqlexecution

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
	"testing"
)

func TestDirectSynchronousEvaluationPersistenceAndScope(t *testing.T) {
	f := setupPreparation(t)
	direct, err := NewDirect(f.preparer, f.unit)
	if err != nil {
		t.Fatal(err)
	}
	result, err := direct.Evaluate(context.Background(), f.parent, workerItems()[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].Decision == nil || result.Results[0].Decision.Score != 15 || result.Results[0].Decision.ObjectID != " first " {
		t.Fatal(result)
	}
	if f.count(t, "decision_engine_decisions") != 1 || f.count(t, "decision_engine_rule_executions") != 1 || f.count(t, "decision_engine_async_executions") != 0 {
		t.Fatal("direct execution did not persist independently of a queue owner")
	}
	if _, err := direct.Evaluate(context.Background(), uuid.New(), workerItems()[0]); err == nil {
		t.Fatal("foreign scenario accepted")
	}
	f.revoked.Store(true)
	if _, err := direct.Evaluate(context.Background(), f.parent, workerItems()[0]); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked direct authority", err)
	}
}
func TestDirectSynchronousLateFailureRollsBack(t *testing.T) {
	f := setupPreparation(t)
	direct, err := NewDirect(f.preparer, f.unit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".fixture_direct_failure BEFORE INSERT ON " + f.schema + ".decision_engine_rule_executions FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated direct rollback'"); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.Evaluate(context.Background(), f.parent, workerItems()[0]); err == nil {
		t.Fatal("late failure accepted")
	}
	for _, table := range []string{"decision_engine_decisions", "decision_engine_rule_executions", "decision_engine_effect_job_receipts"} {
		if f.count(t, table) != 0 {
			t.Fatal("partial persistence", table)
		}
	}
}
func TestDirectSynchronousModelChangeFailsWithoutWrites(t *testing.T) {
	f := setupPreparation(t)
	direct, err := NewDirect(f.preparer, f.unit)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	f.provider.onRead = func() { f.publish(t, 45) }
	f.provider.mu.Unlock()
	_, err = direct.Evaluate(context.Background(), f.parent, service.DecisionEvaluationRequest{ObjectID: "late", ObjectType: "wallets", Fields: map[string]any{"amount": 10}})
	if !errors.Is(err, mysql.ErrExecutionBindingChanged) {
		t.Fatal("publication race was not fenced", err)
	}
	if f.count(t, "decision_engine_decisions") != 0 {
		t.Fatal("stale result persisted")
	}
}
