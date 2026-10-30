package mysqlexecution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (f *preparationFixture) asyncWorker(t *testing.T) *AsyncWorker {
	t.Helper()
	transport, err := mysql.NewExecutionDeliveryStore(f.db, f.database, f.tenant, ports.JobAsync, string(ports.JobAsync), time.Minute, func(_ context.Context, tx *sql.Tx, tenant uuid.UUID, kind ports.JobKind, action mysql.ExecutionWorkerAction) error {
		if tx == nil || tenant != f.tenant || kind != ports.JobAsync || f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewAsyncWorker(f.executions, transport, f.preparer, f.unit, workerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func (f *preparationFixture) queued(t *testing.T, parent, callback string) string {
	t.Helper()
	id := uuid.NewString()
	body, err := json.Marshal(service.AsyncDecisionExecutionRequest{ScenarioID: parent, ObjectType: "wallets", Items: workerItems(), IdempotencyKey: id, CallbackURL: callback})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.executions.Run(context.Background(), mysql.ExecutionRequest, func(r mysql.ExecutionRepositories) error {
		_, err := r.Async.Create(context.Background(), execution.AsyncDecisionExecution{ID: id, TenantID: f.tenant.String(), ScenarioID: parent, ObjectType: "wallets", Status: execution.StatusQueued, IdempotencyKey: id, MaxAttempts: 3, CreatedAt: clock{}.Now(), CallbackURL: callback, RequestBody: body})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestMySQLAsyncWorkerAllOutputsAndRequiredCallbackAtomic(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	id := f.queued(t, f.parent.String(), "https://callback.invalid/result")
	newVersion := f.publish(t, 45)
	w := f.asyncWorker(t)
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "decision_engine_decisions") != 2 || f.count(t, "decision_engine_rule_executions") != 2 || f.count(t, "decision_engine_effect_job_receipts") != 3 || f.count(t, "decision_engine_execution_job_receipts") != 2 {
		t.Fatal("incomplete outputs or required callback")
	}
	var record mysql.AsyncExecutionRecord
	if err := f.executions.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
		var err error
		record, err = r.Async.GetByID(ctx, f.tenant.String(), id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var result service.DecisionEvaluationResult
	if err := json.Unmarshal(record.ResultBody, &result); err != nil || record.Status != execution.StatusCompleted || record.CallbackStatus != "pending" || result.Decision == nil || result.Decision.ObjectID != "second" || result.Decision.Score != 45 || result.Decision.ScenarioIterationID != newVersion.String() {
		t.Fatal(record, result, err)
	}
	if err := task.Execute(ctx); err == nil {
		t.Fatal("completed async executed twice")
	}
}
func TestMySQLAsyncWorkerLiveScenarioBatchAndEmptySelection(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	first := f.parent
	f.parent = uuid.New()
	at := clock{}.Now()
	if err := f.authoring.Mutate(ctx, func(r mysql.AuthoringRepositories) error {
		_, err := r.Scenarios.Create(ctx, scenario.Scenario{ID: f.parent.String(), TenantID: f.tenant.String(), Name: "Second", Description: "test", TriggerObjectType: "wallets", CreatedAt: at, UpdatedAt: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.publish(t, 25)
	f.parent = first
	id := f.queued(t, "", "")
	w := f.asyncWorker(t)
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "decision_engine_decisions") != 4 {
		t.Fatal("all-live fanout lost outputs")
	}
	var body []byte
	if err := f.db.QueryRow("SELECT result_body FROM "+f.schema+".decision_engine_async_executions WHERE id=?", mysqlstore.UUID(uuid.MustParse(id))).Scan(&body); err != nil {
		t.Fatal(err)
	}
	var result service.MultiScenarioEvaluationResult
	if err := json.Unmarshal(body, &result); err != nil || len(result.Results) != 2 || result.ObjectID != "second" {
		t.Fatal(result, err)
	}
	for _, r := range result.Results {
		if r.Decision == nil || r.Decision.TenantID != f.tenant.String() {
			t.Fatal("foreign/missing result", r)
		}
	}
	// Unpublished all-live selection legitimately yields zero decisions, but
	// invalid payloads are still validated and do not become successful no-match.
	if _, err := f.db.Exec("DELETE FROM "+f.schema+".decision_engine_scenario_live_iterations WHERE tenant_id=?", mysqlstore.UUID(f.tenant)); err != nil {
		t.Fatal(err)
	}
	f.queued(t, "", "")
	task, err = w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "decision_engine_decisions") != 4 {
		t.Fatal("empty selection fabricated decision")
	}
}
func TestMySQLAsyncWorkerRequiredCallbackFailureRollsBackAllOutputs(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	f.queued(t, f.parent.String(), "https://callback.invalid/result")
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".callback_failure BEFORE INSERT ON " + f.schema + ".decision_engine_execution_job_receipts FOR EACH ROW BEGIN IF NEW.kind='async_decision_execution_callback' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected callback failure'; END IF; END"); err != nil {
		t.Fatal(err)
	}
	w := f.asyncWorker(t)
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err == nil {
		t.Fatal("required callback failure ignored")
	}
	if f.count(t, "decision_engine_decisions") != 0 || f.count(t, "decision_engine_effect_job_receipts") != 0 {
		t.Fatal("outputs survived callback rollback")
	}
	var state string
	var absent bool
	if err := f.db.QueryRow("SELECT status,result_body IS NULL FROM "+f.schema+".decision_engine_async_executions").Scan(&state, &absent); err != nil || state != "running" || !absent {
		t.Fatal(state, absent, err)
	}
	if _, err := f.db.Exec("DROP TRIGGER " + f.schema + ".callback_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE " + f.schema + ".decision_engine_execution_job_deliveries SET available_at=UTC_TIMESTAMP(6)"); err != nil {
		t.Fatal(err)
	}
	task, err = w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "decision_engine_decisions") != 2 {
		t.Fatal("retry duplicate/missing decisions")
	}
}
func TestMySQLEvaluationWorkerScopeAndCancellation(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	id := f.queued(t, f.parent.String(), "")
	w := f.asyncWorker(t)
	if _, err := NewScheduledWorker(f.executions, w.transport, f.preparer, f.unit, workerPolicy()); err == nil {
		t.Fatal("cross-kind transport accepted")
	}
	wrong := *f.preparer
	wrong.executions = nil
	if _, err := NewAsyncWorker(f.executions, w.transport, &wrong, f.unit, workerPolicy()); err == nil {
		t.Fatal("unbound preparer accepted")
	}
	f.provider.onRead = func() {
		f.provider.onRead = nil
		if err := f.executions.Run(ctx, mysql.ExecutionCancel, func(r mysql.ExecutionRepositories) error {
			_, err := r.Async.Cancel(ctx, uuid.MustParse(id), 0)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); !errors.Is(err, mysql.ErrExecutionLease) && !errors.Is(err, mysql.ErrExecutionState) {
		t.Fatal("cancelled batch accepted", err)
	}
	if f.count(t, "decision_engine_decisions") != 0 {
		t.Fatal("cancelled async outputs persisted")
	}
}
