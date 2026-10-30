package mysql

import (
	"context"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLExecutionCancellationRequiredCallbackAndStaleWorkers(t *testing.T) {
	f, owner, tenant, parent, iteration := executionFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	for _, running := range []bool{false, true} {
		input := scheduledInput(tenant, parent, iteration)
		if err := owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Scheduled.Create(ctx, input); return err }); err != nil {
			t.Fatal(err)
		}
		worker := executionTransportFixture(t, f, tenant, ports.JobScheduled)
		var lease *ExecutionLease
		if running {
			var err error
			lease, err = worker.Claim(ctx)
			if err != nil || lease == nil {
				t.Fatal(lease, err)
			}
			if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Scheduled.Start(ctx, *lease); return err }); err != nil {
				t.Fatal(err)
			}
		}
		var cancelled ScheduledExecutionRecord
		if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
			var err error
			cancelled, err = r.Scheduled.Cancel(ctx, uuid.MustParse(input.ID), 0)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if cancelled.Status != ExecutionStatusCancelled || cancelled.CancelledAt == nil || cancelled.FailedAt != nil {
			t.Fatal(cancelled)
		}
		if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
			again, err := r.Scheduled.Cancel(ctx, uuid.MustParse(input.ID), 0)
			if err == nil && (!again.CancelledAt.Equal(*cancelled.CancelledAt) || again.Generation != 0) {
				t.Error("repeat cancellation mutated state", again)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
			_, err := r.Scheduled.Cancel(ctx, uuid.MustParse(input.ID), 1)
			return err
		}); err == nil {
			t.Fatal("stale generation accepted")
		}
		if running {
			if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Scheduled.Complete(ctx, *lease) }); err == nil {
				t.Fatal("cancelled worker completed")
			}
			if err := worker.Heartbeat(ctx, *lease); err == nil {
				t.Fatal("cancelled token renewed")
			}
		}
		if err := executionTestStore(t, f, uuid.New()).Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
			_, err := r.Scheduled.Cancel(ctx, uuid.MustParse(input.ID), 0)
			return err
		}); err == nil {
			t.Fatal("foreign tenant cancelled owner")
		}
	}
	async := asyncInput(tenant, parent)
	if err := owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, async); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_cancel_callback BEFORE INSERT ON " + schema + ".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cancellation callback unavailable'"); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
		_, _ = r.Async.Cancel(ctx, uuid.MustParse(async.ID), 0)
		return nil
	}); err == nil {
		t.Fatal("ignored cancellation callback failure committed")
	}
	if _, err := f.db.Exec("DROP TRIGGER " + schema + ".fixture_cancel_callback"); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		got, err := r.Async.GetByID(ctx, tenant.String(), async.ID)
		if err == nil && (got.Status != execution.StatusQueued || got.CancelledAt != nil) {
			t.Error("owner cancellation did not roll back", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
			got, err := r.Async.Cancel(ctx, uuid.MustParse(async.ID), 0)
			if err == nil && (got.Status != ExecutionStatusCancelled || got.CallbackStatus != "pending" || got.CancelledAt == nil) {
				t.Error(got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var callbacks int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_execution_job_receipts WHERE tenant_id=? AND resource_id=? AND kind=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(async.ID)), string(ports.JobCallback)).Scan(&callbacks); err != nil || callbacks != 1 {
		t.Fatal("cancellation created duplicate/missing callback", callbacks, err)
	}
	cb := executionTransportFixture(t, f, tenant, ports.JobCallback)
	lease, err := cb.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Async.StartCallback(ctx, *lease); return err }); err != nil {
		t.Fatal("cancelled owner's required callback cannot run", err)
	}
	if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
		got, err := r.Async.CancelCallback(ctx, uuid.MustParse(async.ID), 0)
		if err == nil && (got.Status != ExecutionStatusCancelled || got.CallbackStatus != "cancelled" || got.CallbackCancelledAt == nil) {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Async.CallbackOutcome(ctx, *lease, true, "", nil) }); err == nil {
		t.Fatal("cancelled callback acknowledged")
	}
	if err := owner.Run(ctx, ExecutionRedrive, func(r ExecutionRepositories) error {
		got, err := r.Async.RedriveCallback(ctx, uuid.MustParse(async.ID), 0)
		if err == nil && (got.Status != ExecutionStatusCancelled || got.Generation != 1 || got.CallbackCancelledAt != nil || got.CancelledAt == nil) {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLCallbackCancellationPreservesCompletedEvaluation(t *testing.T) {
	f, owner, tenant, parent, _ := executionFixture(t)
	ctx := context.Background()
	input := asyncInput(tenant, parent)
	if err := owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, input); return err }); err != nil {
		t.Fatal(err)
	}
	worker := executionTransportFixture(t, f, tenant, ports.JobAsync)
	lease, err := worker.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		if _, err := r.Async.Start(ctx, *lease); err != nil {
			return err
		}
		return r.Async.Complete(ctx, *lease, []byte(`{"score":45}`))
	}); err != nil {
		t.Fatal(err)
	}
	var before AsyncExecutionRecord
	if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		var err error
		before, err = r.Async.GetByID(ctx, tenant.String(), input.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
		got, err := r.Async.CancelCallback(ctx, uuid.MustParse(input.ID), 0)
		if err == nil && (got.Status != execution.StatusCompleted || string(got.ResultBody) != string(before.ResultBody) || got.CompletedAt == nil || !got.CompletedAt.Equal(*before.CompletedAt) || got.AttemptCount != before.AttemptCount) {
			t.Error("callback cancellation damaged evaluation", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error {
		_, err := r.Async.Cancel(ctx, uuid.MustParse(input.ID), 0)
		return err
	}); err == nil {
		t.Fatal("completed evaluation cancelled")
	}
}
