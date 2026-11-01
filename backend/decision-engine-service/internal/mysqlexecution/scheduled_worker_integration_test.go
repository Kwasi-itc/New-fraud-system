package mysqlexecution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func workerPolicy() EvaluationWorkerPolicy {
	return EvaluationWorkerPolicy{Concurrency: 1, PollInterval: 10 * time.Millisecond, ClaimTimeout: time.Second, HeartbeatInterval: time.Second, HeartbeatTimeout: time.Second, WorkTimeout: 10 * time.Second, DomainRetryBackoff: time.Second}
}
func (f *preparationFixture) worker(t *testing.T) *ScheduledWorker {
	t.Helper()
	transport, err := mysql.NewExecutionDeliveryStore(f.db, f.database, f.tenant, ports.JobScheduled, string(ports.JobScheduled), time.Minute, func(_ context.Context, tx *sql.Tx, tenant uuid.UUID, kind ports.JobKind, action mysql.ExecutionWorkerAction) error {
		if tx == nil || tenant != f.tenant || kind != ports.JobScheduled || f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewScheduledWorker(f.executions, transport, f.preparer, f.unit, workerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func (f *preparationFixture) pending(t *testing.T, items []service.DecisionEvaluationRequest) mysql.ExecutionLease {
	t.Helper()
	id, at := uuid.New(), clock{}.Now()
	body, err := json.Marshal(service.ScheduledExecutionRequest{Items: items, IdempotencyKey: id.String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.executions.Run(context.Background(), mysql.ExecutionRequest, func(r mysql.ExecutionRepositories) error {
		_, err := r.Scheduled.Create(context.Background(), execution.ScheduledExecution{ID: id.String(), TenantID: f.tenant.String(), ScenarioID: f.parent.String(), ScenarioIterationID: f.old.String(), Source: execution.SourceManual, Status: execution.StatusPending, IdempotencyKey: id.String(), MaxAttempts: 3, ScheduledFor: at, CreatedAt: at, RequestBody: body})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lease := mysql.ExecutionLease{Token: uuid.New(), TransportAttempt: 1, Identity: mysqljobs.Identity{Version: 1, Kind: ports.JobScheduled, TenantID: f.tenant, ResourceID: id, DomainAttempt: 1}}
	if err := f.db.QueryRow("SELECT id FROM "+f.schema+".decision_engine_execution_job_receipts WHERE resource_id=?", mysqlstore.UUID(id)).Scan((*mysqlstore.UUID)(&lease.ID)); err != nil {
		t.Fatal(err)
	}
	return lease
}
func workerItems() []service.DecisionEvaluationRequest {
	return []service.DecisionEvaluationRequest{{ObjectID: " first ", ObjectType: "wallets", Fields: map[string]any{"amount": 10}}, {ObjectID: "second", ObjectType: "wallets", Fields: map[string]any{"amount": 20}}}
}
func TestMySQLScheduledWorkerActualClaimWholeBatchAndReconciliation(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	before := f.pending(t, workerItems())
	w := f.worker(t)
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Heartbeat(ctx); err != nil {
		t.Fatal(err)
	}
	if err := task.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "decision_engine_decisions") != 2 || f.count(t, "decision_engine_rule_executions") != 2 || f.count(t, "decision_engine_effect_job_receipts") != 3 {
		t.Fatal("batch/evidence/outbox incomplete")
	}
	if committed, err := f.executions.EvaluationCompletionCommitted(ctx, before); err != nil || !committed {
		t.Fatal(committed, err)
	}
	if err := task.Execute(ctx); err == nil {
		t.Fatal("completed attempt executed twice")
	}
	if task, err := w.Claim(ctx); err != nil || task != nil {
		t.Fatal("completed job claimed", task, err)
	}
	foreign := before
	foreign.Identity.TenantID = uuid.New()
	if _, err := f.executions.EvaluationCompletionCommitted(ctx, foreign); !errors.Is(err, mysql.ErrExecutionLease) {
		t.Fatal("foreign reconciliation", err)
	}
	changed := before
	changed.Identity.RedriveGeneration++
	if committed, err := f.executions.EvaluationCompletionCommitted(ctx, changed); err != nil || committed {
		t.Fatal("different generation confirmed", committed, err)
	}
	// Retention can remove delivery rows, not permanent owner/identity proof.
	if _, err := f.db.Exec("DELETE FROM "+f.schema+".decision_engine_execution_job_deliveries WHERE id=?", mysqlstore.UUID(before.ID)); err != nil {
		t.Fatal(err)
	}
	if committed, err := f.executions.EvaluationCompletionCommitted(ctx, before); err != nil || !committed {
		t.Fatal("cleanup lost completion proof", committed, err)
	}
}
func TestMySQLScheduledWorkerCancellationDuringPreparationWritesNothing(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	lease := f.pending(t, workerItems())
	w := f.worker(t)
	f.provider.onRead = func() {
		f.provider.onRead = nil
		if err := f.executions.Run(ctx, mysql.ExecutionCancel, func(r mysql.ExecutionRepositories) error {
			_, err := r.Scheduled.Cancel(ctx, lease.Identity.ResourceID, 0)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err == nil {
		t.Fatal("cancelled output committed")
	}
	if f.count(t, "decision_engine_decisions") != 0 || f.count(t, "decision_engine_effect_job_receipts") != 0 {
		t.Fatal("partial cancelled outputs")
	}
	if ok, err := f.executions.EvaluationCompletionCommitted(ctx, lease); err != nil || ok {
		t.Fatal("cancellation reported success", ok, err)
	}
}
func TestMySQLScheduledWorkerPersistenceRollbackAndTransportRetry(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	f.pending(t, workerItems())
	w := f.worker(t)
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".worker_injected_failure BEFORE INSERT ON " + f.schema + ".decision_engine_outbox_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected output failure'"); err != nil {
		t.Fatal(err)
	}
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err == nil {
		t.Fatal("required outbox failure ignored")
	}
	if f.count(t, "decision_engine_decisions") != 0 || f.count(t, "decision_engine_rule_executions") != 0 {
		t.Fatal("partial batch persisted")
	}
	var state string
	var attempt int
	if err := f.db.QueryRow("SELECT state,attempt FROM "+f.schema+".decision_engine_execution_job_deliveries").Scan(&state, &attempt); err != nil || state != "available" || attempt != 1 {
		t.Fatal(state, attempt, err)
	}
	if _, err := f.db.Exec("DROP TRIGGER " + f.schema + ".worker_injected_failure"); err != nil {
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
		t.Fatal("retry duplicate/missing outputs")
	}
	var domain int
	if err := f.db.QueryRow("SELECT attempt_count FROM " + f.schema + ".decision_engine_scheduled_executions").Scan(&domain); err != nil || domain != 1 {
		t.Fatal("transport retry consumed domain attempt", domain, err)
	}
}
func TestMySQLScheduledWorkerInvalidInputIsTerminalAndRevocationFailsClosed(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	items := workerItems()
	items[1].Fields["amount"] = "invalid"
	lease := f.pending(t, items)
	w := f.worker(t)
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err == nil {
		t.Fatal("invalid input ignored")
	}
	var status, last string
	if err := f.db.QueryRow("SELECT status,last_error FROM "+f.schema+".decision_engine_scheduled_executions WHERE id=?", mysqlstore.UUID(lease.Identity.ResourceID)).Scan(&status, &last); err != nil || status != string(execution.StatusFailed) || last != "evaluation_input_invalid" {
		t.Fatal(status, last, err)
	}
	if f.count(t, "decision_engine_decisions") != 0 {
		t.Fatal("partial invalid batch")
	}
	f.revoked.Store(true)
	if _, err := w.Claim(ctx); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked claim", err)
	}
}

func TestMySQLScheduledWorkerSharedPollerCompletesAndJoinsOnShutdown(t *testing.T) {
	f := setupPreparation(t)
	lease := f.pending(t, workerItems())
	w := f.worker(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- w.Run(ctx, func(_ context.Context, err error) error { return err }) }() // Any reported error fails this fixture's worker.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-finished:
			t.Fatal("worker exited before completion", err)
		case <-deadline.C:
			t.Fatal("worker did not complete")
		case <-ticker.C:
			ok, err := f.executions.EvaluationCompletionCommitted(ctx, lease)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				cancel()
				select {
				case err := <-finished:
					if !errors.Is(err, context.Canceled) {
						t.Fatal("shutdown not reported", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("worker did not join")
				}
				if f.count(t, "decision_engine_decisions") != 2 {
					t.Fatal("missing outputs")
				}
				return
			}
		}
	}
}
