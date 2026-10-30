package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func TestMySQLExecutionTransportClaimHeartbeatAndOwnerAck(t *testing.T) {
	f, owner, tenant, parent, iteration := executionFixture(t)
	ctx := context.Background()
	newTransport := func(kind ports.JobKind) *ExecutionDeliveryStore {
		t.Helper()
		s, err := NewExecutionDeliveryStore(f.db, f.database, tenant, kind, string(kind), time.Minute, func(_ context.Context, tx *sql.Tx, actual uuid.UUID, actualKind ports.JobKind, action ExecutionWorkerAction) error {
			if tx == nil || actual != tenant || actualKind != kind || action != ExecutionWorkerClaim && action != ExecutionWorkerHeartbeat {
				return ErrDecisionAuthorizationDenied
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
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
	worker := newTransport(ports.JobScheduled)
	var wg sync.WaitGroup
	errs, leases := make(chan error, 3), make(chan *ExecutionLease, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); lease, err := worker.Claim(ctx); errs <- err; leases <- lease }()
	}
	wg.Wait()
	close(errs)
	close(leases)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var lease *ExecutionLease
	for got := range leases {
		if got != nil {
			if lease != nil {
				t.Fatal("delivery claimed concurrently twice")
			}
			lease = got
		}
	}
	if lease == nil || lease.Identity.ResourceID.String() != scheduled.ID || lease.TransportAttempt != 1 {
		t.Fatal("wrong scoped lease", lease)
	}
	forged := *lease
	forged.Identity.ResourceID = uuid.New()
	if err := worker.Heartbeat(ctx, forged); !errors.Is(err, ErrExecutionLease) {
		t.Fatal("forged identity heartbeat accepted", err)
	}
	if err := worker.Heartbeat(ctx, *lease); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		if _, err := r.Scheduled.Start(ctx, *lease); err != nil {
			return err
		}
		return r.Scheduled.Complete(ctx, *lease)
	}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Heartbeat(ctx, *lease); !errors.Is(err, ErrExecutionLease) {
		t.Fatal("acknowledged lease heartbeat accepted", err)
	}
	asyncWorker := newTransport(ports.JobAsync)
	al, err := asyncWorker.Claim(ctx)
	if err != nil || al == nil || al.Identity.ResourceID.String() != async.ID {
		t.Fatal(al, err)
	}
	if err := asyncWorker.Heartbeat(ctx, *al); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		if _, err := r.Async.Start(ctx, *al); err != nil {
			return err
		}
		return r.Async.Complete(ctx, *al, []byte(`{"outcome":"review"}`))
	}); err != nil {
		t.Fatal(err)
	}
	callbackWorker := newTransport(ports.JobCallback)
	cb, err := callbackWorker.Claim(ctx)
	if err != nil || cb == nil || cb.Identity.ResourceID.String() != async.ID {
		t.Fatal(cb, err)
	}
	if err := callbackWorker.Heartbeat(ctx, *cb); err != nil {
		t.Fatal(err)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		if _, err := r.Async.StartCallback(ctx, *cb); err != nil {
			return err
		}
		return r.Async.CallbackOutcome(ctx, *cb, true, "", nil)
	}); err != nil {
		t.Fatal(err)
	}
	denied, err := NewExecutionDeliveryStore(f.db, f.database, tenant, ports.JobAsync, string(ports.JobAsync), time.Minute, func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, ExecutionWorkerAction) error {
		return ErrDecisionAuthorizationDenied
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := denied.Claim(ctx); !errors.Is(err, ErrDecisionAuthorizationDenied) || got != nil {
		t.Fatal("worker authority bypass", got, err)
	}
}
