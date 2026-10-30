package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func executionTransportFixture(t *testing.T, f platformFixture, tenant uuid.UUID, kind ports.JobKind) *ExecutionDeliveryStore {
	t.Helper()
	s, err := NewExecutionDeliveryStore(f.db, f.database, tenant, kind, string(kind), time.Minute, func(_ context.Context, tx *sql.Tx, actual uuid.UUID, actualKind ports.JobKind, action ExecutionWorkerAction) error {
		if tx == nil || tenant != actual || kind != actualKind || action != ExecutionWorkerClaim && action != ExecutionWorkerHeartbeat && action != ExecutionWorkerRescue {
			return ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Fixture time travel avoids waiting 1s then 16s for the real River schedule;
// production retry timing is still computed/stored by the owner implementation.
func executionForceDue(t *testing.T, f platformFixture, lease ExecutionLease) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.database)
	result, err := f.db.Exec("UPDATE "+schema+".decision_engine_execution_job_deliveries SET available_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE tenant_id=? AND id=? AND state='available'", mysqlstore.UUID(lease.Identity.TenantID), mysqlstore.UUID(lease.ID))
	if err := executionChanged(result, err); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLExecutionTransportRetryExhaustionAndRequiredCallbackRollback(t *testing.T) {
	f, owner, tenant, parent, iteration := executionFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	scheduled, async := scheduledInput(tenant, parent, iteration), asyncInput(tenant, parent)
	if err := owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
		if _, err := r.Scheduled.Create(ctx, scheduled); err != nil {
			return err
		}
		_, err := r.Async.Create(ctx, async)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	worker := executionTransportFixture(t, f, tenant, ports.JobScheduled)
	for attempt := 1; attempt <= 3; attempt++ {
		lease, err := worker.Claim(ctx)
		if err != nil || lease == nil || lease.TransportAttempt != attempt {
			t.Fatal(lease, err)
		}
		if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			got, err := r.Scheduled.Start(ctx, *lease)
			if err == nil && got.AttemptCount != 1 {
				t.Error("transport retry consumed a domain attempt", got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		var state workers.DeliveryState
		if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			var err error
			state, err = r.Scheduled.RetryDelivery(ctx, *lease, "transport_failed")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if attempt < 3 {
			if state != workers.Available {
				t.Fatal(state)
			}
			executionForceDue(t, f, *lease)
		} else if state != workers.Discarded {
			t.Fatal(state)
		}
	}
	if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		got, err := r.Scheduled.GetByID(ctx, tenant.String(), parent.String(), scheduled.ID)
		if err == nil && (got.Status != execution.StatusFailed || got.AttemptCount != 1 || got.LastError != "delivery_attempts_exhausted") {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	asyncWorker := executionTransportFixture(t, f, tenant, ports.JobAsync)
	for attempt := 1; attempt <= 3; attempt++ {
		lease, err := asyncWorker.Claim(ctx)
		if err != nil || lease == nil || lease.TransportAttempt != attempt {
			t.Fatal(lease, err)
		}
		// Deliberately never call Start: transport can fail before evaluation.
		if attempt == 3 {
			if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_exhaustion_callback BEFORE INSERT ON " + schema + ".decision_engine_execution_job_deliveries FOR EACH ROW BEGIN IF NEW.kind=_binary'async_decision_execution_callback' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='callback queue failed'; END IF; END"); err != nil {
				t.Fatal(err)
			}
			err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
				_, _ = r.Async.RetryDelivery(ctx, *lease, "transport_failed")
				return nil
			})
			if _, dropErr := f.db.Exec("DROP TRIGGER " + schema + ".fixture_exhaustion_callback"); dropErr != nil {
				t.Fatal(dropErr)
			}
			if err == nil {
				t.Fatal("exhaustion committed without required callback")
			}
			if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
				got, err := r.Async.GetByID(ctx, tenant.String(), async.ID)
				if err == nil && (got.Status != execution.StatusQueued || got.AttemptCount != 0 || got.CallbackStatus != "") {
					t.Error("partial exhaustion committed", got)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		var state workers.DeliveryState
		if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			var err error
			state, err = r.Async.RetryDelivery(ctx, *lease, "transport_failed")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if attempt < 3 {
			if state != workers.Available {
				t.Fatal(state)
			}
			executionForceDue(t, f, *lease)
		} else if state != workers.Discarded {
			t.Fatal(state)
		}
	}
	if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		got, err := r.Async.GetByID(ctx, tenant.String(), async.ID)
		if err == nil && (got.Status != execution.StatusFailed || got.AttemptCount != 0 || got.CallbackStatus != "pending") {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	callbackWorker := executionTransportFixture(t, f, tenant, ports.JobCallback)
	for attempt := 1; attempt <= 3; attempt++ {
		lease, err := callbackWorker.Claim(ctx)
		if err != nil || lease == nil || lease.TransportAttempt != attempt {
			t.Fatal(lease, err)
		}
		if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			_, err := r.Async.RetryDelivery(ctx, *lease, "transport_failed")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if attempt < 3 {
			executionForceDue(t, f, *lease)
		}
	}
	if err := owner.Run(ctx, ExecutionRedrive, func(r ExecutionRepositories) error {
		got, err := r.Async.RedriveCallback(ctx, uuid.MustParse(async.ID), 0)
		if err == nil && (got.Status != execution.StatusFailed || got.AttemptCount != 0 || got.Generation != 1 || got.CallbackAttemptCount != 0 || got.CallbackStatus != "pending") {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var asyncJobs int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_execution_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=?", mysqlstore.UUID(tenant), string(ports.JobAsync), mysqlstore.UUID(uuid.MustParse(async.ID))).Scan(&asyncJobs); err != nil || asyncJobs != 1 {
		t.Fatal("callback redrive reran evaluation", asyncJobs, err)
	}
}

func TestMySQLCallbackOnlyRedrivePreservesCompletedResult(t *testing.T) {
	f, owner, tenant, parent, _ := executionFixture(t)
	ctx := context.Background()
	x := asyncInput(tenant, parent)
	if err := owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, x); return err }); err != nil {
		t.Fatal(err)
	}
	al, err := executionTransportFixture(t, f, tenant, ports.JobAsync).Claim(ctx)
	if err != nil || al == nil {
		t.Fatal(al, err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		if _, err := r.Async.Start(ctx, *al); err != nil {
			return err
		}
		return r.Async.Complete(ctx, *al, []byte(`{"score":45}`))
	}); err != nil {
		t.Fatal(err)
	}
	cb, err := executionTransportFixture(t, f, tenant, ports.JobCallback).Claim(ctx)
	if err != nil || cb == nil {
		t.Fatal(cb, err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		if _, err := r.Async.StartCallback(ctx, *cb); err != nil {
			return err
		}
		return r.Async.CallbackOutcome(ctx, *cb, false, "callback failed", nil)
	}); err != nil {
		t.Fatal(err)
	}
	var before AsyncExecutionRecord
	if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		var err error
		before, err = r.Async.GetByID(ctx, tenant.String(), x.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionRedrive, func(r ExecutionRepositories) error {
		got, err := r.Async.RedriveCallback(ctx, uuid.MustParse(x.ID), 0)
		if err == nil && (got.Status != execution.StatusCompleted || got.AttemptCount != 1 || got.CompletedAt == nil || !got.CompletedAt.Equal(*before.CompletedAt) || string(got.ResultBody) != string(before.ResultBody) || got.Generation != 1 || got.CallbackAttemptCount != 0) {
			t.Error("callback redrive changed evaluation", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionRedrive, func(r ExecutionRepositories) error {
		_, err := r.Async.RedriveCallback(ctx, uuid.MustParse(x.ID), 0)
		return err
	}); !errors.Is(err, ErrExecutionState) {
		t.Fatal("stale callback redrive accepted", err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Async.CallbackOutcome(ctx, *cb, true, "", nil) }); !errors.Is(err, ErrExecutionState) {
		t.Fatal("old generation callback committed", err)
	}
}
