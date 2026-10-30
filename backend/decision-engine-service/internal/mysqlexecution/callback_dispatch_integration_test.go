package mysqlexecution

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func (f *preparationFixture) callbackDispatch(t *testing.T) (*mysql.DispatchStore, *mysql.ExecutionDeliveryStore) {
	t.Helper()
	delivery, err := mysql.NewExecutionDeliveryStore(f.db, f.database, f.tenant, ports.JobCallback, string(ports.JobCallback), time.Minute, func(_ context.Context, tx *sql.Tx, tenant uuid.UUID, kind ports.JobKind, _ mysql.ExecutionWorkerAction) error {
		if tx == nil || tenant != f.tenant || kind != ports.JobCallback || f.revoked.Load() {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := mysql.NewCallbackDispatchStore(f.executions, delivery)
	if err != nil {
		t.Fatal(err)
	}
	return store, delivery
}

func TestMySQLCallbackDispatchLostCommitAcknowledgementReconcilesWithoutResend(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "reconciliation_denied"}[revoke], func(t *testing.T) {
			f := setupPreparation(t)
			id := completeCallbackEvaluation(t, f)
			ctx := context.Background()
			db, arm := testsupport.DecisionCommitFaultPool(t, f.database)
			queues := map[ports.JobKind]mysql.DeliveryQueuePolicy{}
			for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
				queues[kind] = mysql.DeliveryQueuePolicy{Name: string(kind), MaxAttempts: 3}
			}
			owner, err := mysql.NewExecutionStore(db, f.database, f.tenant, 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, mysql.ExecutionAction) error {
				if f.revoked.Load() {
					return mysql.ErrDecisionAuthorizationDenied
				}
				return nil
			}, mysql.ExecutionPolicy{Queues: queues, CallbackMaxAttempts: 3})
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := mysql.NewExecutionDeliveryStore(db, f.database, f.tenant, ports.JobCallback, string(ports.JobCallback), time.Minute, func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, mysql.ExecutionWorkerAction) error {
				if f.revoked.Load() {
					return mysql.ErrDecisionAuthorizationDenied
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			store, err := mysql.NewCallbackDispatchStore(owner, delivery)
			if err != nil {
				t.Fatal(err)
			}
			sends := 0
			provider := receiptProvider{deliver: func(_ context.Context, request ports.DispatchRequest) (ports.DispatchReceipt, error) {
				sends++
				arm(func() {
					if revoke {
						f.revoked.Store(true)
					}
				})
				return accepted(request), nil
			}}
			w, err := NewDispatchWorker(store, provider, workerPolicy())
			if err != nil {
				t.Fatal(err)
			}
			task, err := w.Claim(ctx)
			if err != nil || task == nil {
				t.Fatal(task, err)
			}
			err = task.Execute(ctx)
			if !revoke && err != nil {
				t.Fatal("committed receipt was not reconciled", err)
			}
			if revoke && (err == nil || !ambiguousTransaction(err) || !errors.Is(err, mysql.ErrDecisionAuthorizationDenied)) {
				t.Fatal("unconfirmed commit was replayed or hidden", err)
			}
			if sends != 1 {
				t.Fatal("duplicate external send", sends)
			}
			var state, callback string
			if err := f.db.QueryRow("SELECT status,callback_status FROM "+f.schema+".decision_engine_async_executions WHERE id=?", mysqlstore.UUID(uuid.MustParse(id))).Scan(&state, &callback); err != nil || state != "completed" || callback != "sent" {
				t.Fatal(state, callback, err)
			}
		})
	}
}
func completeCallbackEvaluation(t *testing.T, f *preparationFixture) string {
	t.Helper()
	id := f.queued(t, f.parent.String(), "https://callback.invalid/result")
	task, err := f.asyncWorker(t).Claim(context.Background())
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	return id
}
func accepted(request ports.DispatchRequest) ports.DispatchReceipt {
	return ports.DispatchReceipt{Key: request.Key, PayloadSHA256: request.PayloadSHA256, Outcome: ports.DispatchAccepted, Reference: "provider-accepted"}
}

func TestMySQLCallbackDispatchCrashRescueReconcilesOriginalIntent(t *testing.T) {
	f := setupPreparation(t)
	id := completeCallbackEvaluation(t, f)
	ctx := context.Background()
	store, delivery := f.callbackDispatch(t)
	lease, err := store.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	intent, err := store.Prepare(ctx, *lease)
	if err != nil || intent.Resumed {
		t.Fatal(intent, err)
	}
	// Simulated crash after external acceptance but BEFORE local acknowledgement.
	if _, err := f.db.Exec("UPDATE "+f.schema+".decision_engine_execution_job_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
		t.Fatal(err)
	}
	candidates, err := delivery.Expired(ctx, nil, 10)
	if err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	if state, err := delivery.Rescue(ctx, f.executions, candidates[0]); err != nil || state != workers.Available {
		t.Fatal(state, err)
	}
	if _, err := f.db.Exec("UPDATE "+f.schema+".decision_engine_execution_job_deliveries SET available_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
		t.Fatal(err)
	}
	sends, queries := 0, 0
	provider := receiptProvider{deliver: func(context.Context, ports.DispatchRequest) (ports.DispatchReceipt, error) {
		sends++
		return ports.DispatchReceipt{}, errors.New("must reconcile first")
	}, query: func(_ context.Context, request ports.DispatchRequest) (ports.DispatchReceipt, error) {
		queries++
		if request.Key != intent.Request.Key || string(request.Payload) != string(intent.Request.Payload) {
			t.Error("rescue changed original send")
		}
		return accepted(request), nil
	}}
	w, err := NewDispatchWorker(store, provider, workerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if sends != 0 || queries != 1 {
		t.Fatal(sends, queries)
	}
	if err := f.executions.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
		v, err := r.Async.GetByID(ctx, f.tenant.String(), id)
		if err == nil && (v.CallbackStatus != "sent" || v.CallbackAttemptCount != 1 || v.Status != "completed") {
			t.Error(v)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if committed, err := store.CompletionCommitted(ctx, *lease, accepted(intent.Request)); err != nil || !committed {
		t.Fatal(committed, err)
	}
	if err := store.Complete(ctx, *lease, intent.Request, accepted(intent.Request), time.Second); err == nil {
		t.Fatal("stale pre-crash worker completed")
	}
}

func TestMySQLCallbackDispatchCancellationDuringIOPreservesResult(t *testing.T) {
	f := setupPreparation(t)
	id := completeCallbackEvaluation(t, f)
	ctx := context.Background()
	store, _ := f.callbackDispatch(t)
	provider := receiptProvider{deliver: func(_ context.Context, request ports.DispatchRequest) (ports.DispatchReceipt, error) {
		// This real owner mutation would deadlock/time out if send held its SQL
		// locks. Cancellation cannot undo a provider action already accepted.
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := f.executions.Run(bounded, mysql.ExecutionCancel, func(r mysql.ExecutionRepositories) error {
			_, err := r.Async.CancelCallback(bounded, uuid.MustParse(id), 0)
			return err
		}); err != nil {
			t.Error("provider IO ran under owner locks", err)
		}
		return accepted(request), nil
	}}
	w, err := NewDispatchWorker(store, provider, workerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	task, err := w.Claim(ctx)
	if err != nil || task == nil {
		t.Fatal(task, err)
	}
	if err := task.Execute(ctx); !errors.Is(err, mysql.ErrExecutionState) && !errors.Is(err, mysql.ErrExecutionLease) {
		t.Fatal("cancelled worker acknowledged", err)
	}
	if err := f.executions.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
		v, err := r.Async.GetByID(ctx, f.tenant.String(), id)
		if err == nil && (v.Status != "completed" || v.CallbackStatus != "cancelled" || len(v.ResultBody) == 0) {
			t.Error(v)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
