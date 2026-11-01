package mysqlexecution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/payload"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// EvaluationWorkerPolicy is explicit deployment configuration, never inferred
// from a claim payload. The constructor checks the actual transport lease and
// reuses the shared bounded poller/supervisor rather than creating another loop.
type EvaluationWorkerPolicy struct {
	Concurrency                                      int
	PollInterval, PollJitter, ClaimTimeout           time.Duration
	HeartbeatInterval, HeartbeatTimeout, WorkTimeout time.Duration
	DomainRetryBackoff                               time.Duration
}

// ScheduledWorker is an opt-in composition component, NOT registered by the
// PostgreSQL worker command. All stores must be built with real trusted service
// identity providers before a deployment can call Run.
type ScheduledWorker struct {
	owner     *mysql.ExecutionStore
	transport *mysql.ExecutionDeliveryStore
	preparer  *Preparer
	unit      *mysql.DecisionUnitOfWorkStore
	poller    *workers.DeliveryPoller
	policy    EvaluationWorkerPolicy
}

func NewScheduledWorker(owner *mysql.ExecutionStore, transport *mysql.ExecutionDeliveryStore, preparer *Preparer, unit *mysql.DecisionUnitOfWorkStore, policy EvaluationWorkerPolicy) (*ScheduledWorker, error) {
	lease, err := owner.ValidateEvaluationWorkerScope(unit, transport, ports.JobScheduled)
	if err != nil {
		return nil, err
	}
	if preparer == nil || preparer.executions != owner || policy.DomainRetryBackoff <= 0 || policy.DomainRetryBackoff > 24*time.Hour {
		return nil, service.ErrExecutionPreparation
	}
	poller, err := newEvaluationPoller(lease, policy)
	if err != nil {
		return nil, err
	}
	return &ScheduledWorker{owner, transport, preparer, unit, poller, policy}, nil
}

func newEvaluationPoller(lease time.Duration, policy EvaluationWorkerPolicy) (*workers.DeliveryPoller, error) {
	if policy.DomainRetryBackoff <= 0 || policy.DomainRetryBackoff > 24*time.Hour {
		return nil, service.ErrExecutionPreparation
	}
	supervisor, err := workers.NewLeaseSupervisor(lease, policy.HeartbeatInterval, policy.HeartbeatTimeout, policy.WorkTimeout)
	if err != nil {
		return nil, err
	}
	poller, err := workers.NewDeliveryPoller(policy.Concurrency, policy.PollInterval, policy.ClaimTimeout, supervisor)
	if err != nil {
		return nil, err
	}
	poller, err = poller.WithPollJitter(policy.PollJitter)
	if err != nil {
		return nil, err
	}
	return poller, nil
}

func (w *ScheduledWorker) Run(ctx context.Context, report func(context.Context, error) error) error {
	if w == nil || w.poller == nil {
		return mysql.ErrDecisionAuthorizationUnavailable
	}
	return w.poller.Run(ctx, w.Claim, report)
}

// Claim returns only committed claims. An ambiguous claim commit returns no
// task; shared transport expiry/rescue, not this process, resolves it.
func (w *ScheduledWorker) Claim(ctx context.Context) (*workers.DeliveryTask, error) {
	if w == nil || w.transport == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	lease, err := w.transport.Claim(ctx)
	if err != nil || lease == nil {
		return nil, err
	}
	return &workers.DeliveryTask{
		Heartbeat: func(ctx context.Context) error { return w.transport.Heartbeat(ctx, *lease) },
		Execute:   func(ctx context.Context) error { return w.execute(ctx, *lease) },
	}, nil
}

func (w *ScheduledWorker) execute(ctx context.Context, lease mysql.ExecutionLease) error {
	var maximum int
	if err := w.owner.Run(ctx, mysql.ExecutionRun, func(r mysql.ExecutionRepositories) error {
		v, err := r.Scheduled.Start(ctx, lease)
		maximum = v.MaxAttempts
		return err
	}); err != nil {
		return fmt.Errorf("start scheduled attempt: %w", err)
	}
	prepared, err := w.preparer.PrepareScheduled(ctx, lease.Identity)
	if err != nil {
		return w.fail(ctx, lease, maximum, err)
	}
	if prepared.Identity() != lease.Identity {
		return service.ErrExecutionPreparation
	}
	event, eventErr := w.preparer.evaluator.PrepareExecutionEvent(lease.Identity.TenantID.String(), "scheduled_execution", lease.Identity.ResourceID.String(), "scheduled_execution.completed", map[string]any{"status": "completed", "scenario_id": prepared.pin.ScenarioID(), "attempt_count": lease.Identity.DomainAttempt})
	if eventErr != nil {
		return w.fail(ctx, lease, maximum, eventErr)
	}
	err = w.unit.CompleteScheduled(ctx, lease, func(store ports.DecisionMutationStore) error {
		_, err := prepared.PersistAll(ctx, store)
		if err != nil {
			return err
		}
		return event.Persist(ctx, store)
	})
	if err == nil {
		return nil
	}
	if ambiguousTransaction(err) {
		// No mutation follows an uncertain commit, even when the read cannot
		// confirm success. Keep the original error and let rescue reconcile.
		return reconcileEvaluationCompletion(ctx, err, func(ctx context.Context) (bool, error) {
			return w.owner.EvaluationCompletionCommitted(ctx, lease)
		})
	}
	if ctx.Err() != nil || errors.Is(err, mysql.ErrExecutionLease) || errors.Is(err, mysql.ErrExecutionState) {
		return err
	}
	// A confirmed persistence rollback consumes a transport failure, not a new
	// business attempt. Plans are single-use and never retried in this call.
	retryErr := w.owner.Run(ctx, mysql.ExecutionRun, func(r mysql.ExecutionRepositories) error {
		_, e := r.Scheduled.RetryDelivery(ctx, lease, "output_persistence_failed")
		return e
	})
	return errors.Join(err, retryErr)
}

func reconcileEvaluationCompletion(ctx context.Context, cause error, read func(context.Context) (bool, error)) error {
	if !ambiguousTransaction(cause) || read == nil {
		return cause
	}
	committed, err := read(ctx)
	if err == nil && committed {
		return nil
	}
	return errors.Join(cause, err)
}

func ambiguousTransaction(err error) bool {
	var commit *mysqlstore.CommitError
	var rollback *mysqlstore.RollbackError
	return errors.As(err, &commit) || errors.As(err, &rollback)
}

func (w *ScheduledWorker) fail(ctx context.Context, lease mysql.ExecutionLease, maximum int, cause error) error {
	if ctx.Err() != nil || ambiguousTransaction(cause) || errors.Is(cause, mysql.ErrExecutionState) || errors.Is(cause, mysql.ErrExecutionLease) {
		return cause
	}
	code, terminal := preparationFailure(cause)
	var next *time.Time
	if !terminal && lease.Identity.DomainAttempt < maximum {
		// Fixed configured delay avoids overflow; the repository checks this
		// instant and atomically allocates a new domain delivery identity.
		at := time.Now().UTC().Add(w.policy.DomainRetryBackoff)
		next = &at
	}
	status, eventType := "failed", "scheduled_execution.failed"
	if next != nil {
		status, eventType = "pending", "scheduled_execution.retry_scheduled"
	}
	event, eventErr := w.preparer.evaluator.PrepareExecutionEvent(lease.Identity.TenantID.String(), "scheduled_execution", lease.Identity.ResourceID.String(), eventType, map[string]any{"status": status, "attempt_count": lease.Identity.DomainAttempt, "max_attempts": maximum, "last_error": code, "next_attempt_at": next})
	if eventErr != nil {
		return errors.Join(cause, eventErr)
	}
	err := w.unit.Run(ctx, mysql.ExecutionRun, func(u mysql.DecisionUnitOfWork) error {
		r, err := u.Executions()
		if err != nil {
			return err
		}
		if err := r.Scheduled.Fail(ctx, lease, next, code); err != nil {
			return err
		}
		return event.Persist(ctx, u.DecisionMutations())
	})
	// Original errors remain observable through the mandatory poller reporter;
	// raw payloads/provider messages are never stored in owner last_error.
	return errors.Join(cause, err)
}

func preparationFailure(err error) (string, bool) {
	var invalid *payload.Error
	if errors.Is(err, service.ErrExecutionPreparation) || errors.Is(err, service.ErrExecutionPreparationLimit) || errors.Is(err, ports.ErrAggregateCapability) || errors.As(err, &invalid) {
		return "evaluation_input_invalid", true
	}
	return "evaluation_preparation_failed", false
}
