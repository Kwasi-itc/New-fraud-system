package mysqlexecution

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func (f *preparationFixture) admission(t *testing.T) *Admission {
	t.Helper()
	a, err := NewAdmission(f.preparer, f.unit, ids{}, clock{}, AdmissionPolicy{ScheduledMaxAttempts: 3, AsyncMaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestMySQLAdmissionActualCreationReplayAndExecution(t *testing.T) {
	f := setupPreparation(t)
	a := f.admission(t)
	ctx := context.Background()
	created, err := a.CreateScheduled(ctx, f.tenant.String(), f.parent.String(), clock{}.Now(), service.ScheduledExecutionRequest{Items: workerItems(), IdempotencyKey: "scheduled-key"})
	if err != nil || created.Replay || created.Record.ScenarioIterationID != f.old.String() {
		t.Fatal(created, err)
	}
	async, err := a.CreateAsync(ctx, f.tenant.String(), service.AsyncDecisionExecutionRequest{ScenarioID: f.parent.String(), ObjectType: "wallets", Items: workerItems(), IdempotencyKey: "async-key"})
	if err != nil || async.Replay {
		t.Fatal(async, err)
	}
	if f.count(t, "decision_engine_execution_job_receipts") != 2 || f.count(t, "decision_engine_effect_job_receipts") != 2 || f.count(t, "decision_engine_decisions") != 0 {
		t.Fatal("admission jobs/events not atomic or evaluated early")
	}
	for _, claim := range []func(context.Context) (*workers.DeliveryTask, error){f.worker(t).Claim, f.asyncWorker(t).Claim} {
		task, err := claim(ctx)
		if err != nil || task == nil {
			t.Fatal(task, err)
		}
		if err := task.Execute(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if f.count(t, "decision_engine_decisions") != 4 {
		t.Fatal("admitted requests not executable")
	}
	beforeCalls := f.provider.calls
	if _, err := f.db.Exec("UPDATE " + f.schema + ".data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
	scheduledReplay, err := a.CreateScheduled(ctx, f.tenant.String(), f.parent.String(), clock{}.Now(), service.ScheduledExecutionRequest{IdempotencyKey: "scheduled-key"})
	if err != nil || !scheduledReplay.Replay || scheduledReplay.Record.ID != created.Record.ID {
		t.Fatal(scheduledReplay, err)
	}
	asyncReplay, err := a.CreateAsync(ctx, f.tenant.String(), service.AsyncDecisionExecutionRequest{ScenarioID: f.parent.String(), ObjectType: "wallets", IdempotencyKey: "async-key"})
	if err != nil || !asyncReplay.Replay || asyncReplay.Record.ID != async.Record.ID || f.provider.calls != beforeCalls {
		t.Fatal("replay depended on changed model", asyncReplay, err)
	}
	if f.count(t, "decision_engine_execution_job_receipts") != 2 || f.count(t, "decision_engine_outbox_events") != 8 {
		t.Fatal("replay duplicated work/events")
	}
}
func TestMySQLAdmissionFencesPublicationModelAndRevocation(t *testing.T) {
	f := setupPreparation(t)
	ctx := context.Background()
	prepared, err := f.preparer.prepareRequest(ctx, f.tenant.String(), f.parent.String(), "", workerItems(), true)
	if err != nil {
		t.Fatal(err)
	}
	input := execution.ScheduledExecution{ID: uuid.NewString(), TenantID: f.tenant.String(), ScenarioID: f.parent.String(), ScenarioIterationID: f.old.String(), Source: execution.SourceManual, Status: execution.StatusPending, MaxAttempts: 3, ScheduledFor: clock{}.Now(), CreatedAt: clock{}.Now(), RequestBody: []byte(`{"items":[]}`)}
	lifecycle := func(ports.DecisionMutationStore) error { return nil } // Fixture disables optional event only.
	f.publish(t, 50)
	if _, err := f.unit.AdmitScheduled(ctx, f.preparer.reader, *prepared.snapshot, prepared.model, input, lifecycle); !errors.Is(err, mysql.ErrExecutionBindingChanged) {
		t.Fatal("publication race accepted", err)
	}
	prepared, err = f.preparer.prepareRequest(ctx, f.tenant.String(), f.parent.String(), "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	input.ScenarioIterationID = prepared.pin.IterationID()
	if _, err := f.db.Exec("UPDATE " + f.schema + ".data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.unit.AdmitScheduled(ctx, f.preparer.reader, *prepared.snapshot, prepared.model, input, lifecycle); err == nil {
		t.Fatal("closed registry accepted")
	}
	f.revoked.Store(true)
	if _, err := f.unit.AdmitScheduled(ctx, f.preparer.reader, *prepared.snapshot, prepared.model, input, lifecycle); !errors.Is(err, mysql.ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked final authority", err)
	}
	if f.count(t, "decision_engine_scheduled_executions") != 0 || f.count(t, "decision_engine_execution_job_receipts") != 0 {
		t.Fatal("failed admission wrote owner/job")
	}
}
func TestMySQLAdmissionRequiredQueuedEventFailureRollsBackAndConcurrentReplay(t *testing.T) {
	f := setupPreparation(t)
	a := f.admission(t)
	ctx := context.Background()
	request := service.AsyncDecisionExecutionRequest{ObjectType: "wallets", Items: workerItems(), IdempotencyKey: "one-admission"}
	if _, err := f.db.Exec("CREATE TRIGGER " + f.schema + ".admission_failure BEFORE INSERT ON " + f.schema + ".decision_engine_outbox_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected queued event failure'"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateAsync(ctx, f.tenant.String(), request); err == nil {
		t.Fatal("queued event failure ignored")
	}
	if f.count(t, "decision_engine_async_executions") != 0 || f.count(t, "decision_engine_execution_job_receipts") != 0 {
		t.Fatal("partial admission")
	}
	if _, err := f.db.Exec("DROP TRIGGER " + f.schema + ".admission_failure"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	results := make(chan mysql.AsyncCreation, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := a.CreateAsync(ctx, f.tenant.String(), request)
			errs <- err
			results <- v
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id, replays := "", 0
	for result := range results {
		if id != "" && id != result.Record.ID {
			t.Fatal("concurrent replay split identity")
		}
		id = result.Record.ID
		if result.Replay {
			replays++
		}
	}
	if replays != 1 || f.count(t, "decision_engine_async_executions") != 1 || f.count(t, "decision_engine_execution_job_receipts") != 1 || f.count(t, "decision_engine_outbox_events") != 1 {
		t.Fatal("duplicate concurrent admission", replays)
	}
}
func TestMySQLAdmissionCandidatesBoundsAndForeignTenant(t *testing.T) {
	f := setupPreparation(t)
	a := f.admission(t)
	ctx := context.Background()
	if _, err := a.CreateScheduled(ctx, f.tenant.String(), f.parent.String(), clock{}.Now(), service.ScheduledExecutionRequest{CandidateLimit: 2}); err != nil {
		t.Fatal("candidate admission", err)
	}
	if _, err := a.CreateAsync(ctx, uuid.NewString(), service.AsyncDecisionExecutionRequest{ObjectType: "wallets", Items: workerItems()}); err == nil {
		t.Fatal("foreign admission")
	}
	if _, err := a.CreateAsync(ctx, f.tenant.String(), service.AsyncDecisionExecutionRequest{ObjectType: "wallets", Items: nil}); err == nil {
		t.Fatal("empty async accepted")
	}
	if _, err := a.CreateAsync(ctx, f.tenant.String(), service.AsyncDecisionExecutionRequest{ObjectType: "wallets", Items: workerItems(), CallbackURL: "file:///tmp/test"}); err == nil {
		t.Fatal("invalid callback URL")
	}
	if _, err := a.CreateScheduled(ctx, f.tenant.String(), f.parent.String(), clock{}.Now(), service.ScheduledExecutionRequest{CandidateLimit: 11}); err == nil {
		t.Fatal("candidate budget ignored")
	}
}
