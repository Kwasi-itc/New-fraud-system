package mysqlexecution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// AsyncWorker is explicitly constructed but never auto-registered by the
// existing PostgreSQL command. Required callback insertion is part of CompleteAsync,
// not a best-effort operation after acknowledgement. Callback delivery itself
// requires its separate trusted provider/HTTP policy.
type AsyncWorker struct {
	owner     *mysql.ExecutionStore
	transport *mysql.ExecutionDeliveryStore
	preparer  *Preparer
	unit      *mysql.DecisionUnitOfWorkStore
	poller    *workers.DeliveryPoller
	policy    EvaluationWorkerPolicy
}

func NewAsyncWorker(owner *mysql.ExecutionStore, transport *mysql.ExecutionDeliveryStore, preparer *Preparer, unit *mysql.DecisionUnitOfWorkStore, policy EvaluationWorkerPolicy) (*AsyncWorker, error) {
	lease, err := owner.ValidateEvaluationWorkerScope(unit, transport, ports.JobAsync)
	if err != nil {
		return nil, err
	}
	if preparer == nil || preparer.executions != owner {
		return nil, service.ErrExecutionPreparation
	}
	poller, err := newEvaluationPoller(lease, policy)
	if err != nil {
		return nil, err
	}
	return &AsyncWorker{owner, transport, preparer, unit, poller, policy}, nil
}
func (w *AsyncWorker) Run(ctx context.Context, report func(context.Context, error) error) error {
	if w == nil || w.poller == nil {
		return mysql.ErrDecisionAuthorizationUnavailable
	}
	return w.poller.Run(ctx, w.Claim, report)
}
func (w *AsyncWorker) Claim(ctx context.Context) (*workers.DeliveryTask, error) {
	if w == nil || w.transport == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	lease, err := w.transport.Claim(ctx)
	if err != nil || lease == nil {
		return nil, err
	}
	return &workers.DeliveryTask{Heartbeat: func(ctx context.Context) error { return w.transport.Heartbeat(ctx, *lease) }, Execute: func(ctx context.Context) error { return w.execute(ctx, *lease) }}, nil
}
func (w *AsyncWorker) execute(ctx context.Context, lease mysql.ExecutionLease) error {
	var maximum int
	if err := w.owner.Run(ctx, mysql.ExecutionRun, func(r mysql.ExecutionRepositories) error {
		v, err := r.Async.Start(ctx, lease)
		maximum = v.MaxAttempts
		return err
	}); err != nil {
		return fmt.Errorf("start async attempt: %w", err)
	}
	prepared, err := w.preparer.PrepareAsync(ctx, lease.Identity)
	if err != nil {
		return w.fail(ctx, lease, maximum, err)
	}
	if prepared.Identity() != lease.Identity {
		return service.ErrExecutionPreparation
	}
	event, eventErr := w.preparer.evaluator.PrepareExecutionEvent(lease.Identity.TenantID.String(), "async_decision_execution", lease.Identity.ResourceID.String(), "async_decision_execution.completed", map[string]any{"status": "completed", "attempt_count": lease.Identity.DomainAttempt})
	if eventErr != nil {
		return w.fail(ctx, lease, maximum, eventErr)
	}
	// Result serialization needs the real persisted output values, but is bounded
	// local CPU work. Completion accepts it through the callback below in this
	// SAME transaction; never commit decisions and result in separate calls.
	err = w.unit.CompleteAsyncPrepared(ctx, lease, func(store ports.DecisionMutationStore) ([]byte, error) {
		body, err := prepared.PersistAll(ctx, store)
		if err != nil {
			return nil, err
		}
		if err := event.Persist(ctx, store); err != nil {
			return nil, err
		}
		return body, nil
	})
	if err == nil {
		return nil
	}
	if ambiguousTransaction(err) {
		return reconcileEvaluationCompletion(ctx, err, func(ctx context.Context) (bool, error) { return w.owner.EvaluationCompletionCommitted(ctx, lease) })
	}
	if ctx.Err() != nil || errors.Is(err, mysql.ErrExecutionLease) || errors.Is(err, mysql.ErrExecutionState) {
		return err
	}
	retryErr := w.owner.Run(ctx, mysql.ExecutionRun, func(r mysql.ExecutionRepositories) error {
		_, e := r.Async.RetryDelivery(ctx, lease, "output_persistence_failed")
		return e
	})
	return errors.Join(err, retryErr)
}
func (w *AsyncWorker) fail(ctx context.Context, lease mysql.ExecutionLease, maximum int, cause error) error {
	if ctx.Err() != nil || ambiguousTransaction(cause) || errors.Is(cause, mysql.ErrExecutionState) || errors.Is(cause, mysql.ErrExecutionLease) {
		return cause
	}
	code, terminal := preparationFailure(cause)
	var next *time.Time
	if !terminal && lease.Identity.DomainAttempt < maximum {
		at := time.Now().UTC().Add(w.policy.DomainRetryBackoff)
		next = &at
	}
	status, eventType := "failed", "async_decision_execution.failed"
	if next != nil {
		status, eventType = "queued", "async_decision_execution.retry_scheduled"
	}
	event, eventErr := w.preparer.evaluator.PrepareExecutionEvent(lease.Identity.TenantID.String(), "async_decision_execution", lease.Identity.ResourceID.String(), eventType, map[string]any{"status": status, "attempt_count": lease.Identity.DomainAttempt, "max_attempts": maximum, "last_error": code, "next_attempt_at": next})
	if eventErr != nil {
		return errors.Join(cause, eventErr)
	}
	err := w.unit.Run(ctx, mysql.ExecutionRun, func(u mysql.DecisionUnitOfWork) error {
		r, err := u.Executions()
		if err != nil {
			return err
		}
		// Redacted JSON error, never provider body/payload. Fail atomically
		// schedules the domain retry OR the required terminal callback.
		if err := r.Async.Fail(ctx, lease, next, code, []byte(`{"error":"evaluation_failed"}`)); err != nil {
			return err
		}
		return event.Persist(ctx, u.DecisionMutations())
	})
	return errors.Join(cause, err)
}
