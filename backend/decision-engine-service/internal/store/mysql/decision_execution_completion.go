package mysql

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// CompleteScheduled persists ALREADY EVALUATED outputs and their required jobs
// with the execution acknowledgement. Evaluation/HTTP preparation must happen
// before this short transaction. A whole scheduled batch belongs in one call,
// not one completion per item. The caller must not reuse the old worker's
// EvaluateScenario-and-then-Complete loop, which commits outputs independently.
func (s *DecisionUnitOfWorkStore) CompleteScheduled(ctx context.Context, lease ExecutionLease, persist func(ports.DecisionMutationStore) error) error {
	if persist == nil {
		return errors.New("prepared scheduled-output persistence callback required")
	}
	return s.Run(ctx, ExecutionRun, func(u DecisionUnitOfWork) error {
		r, err := u.Executions()
		if err != nil {
			return err
		}
		owner, err := r.Scheduled.locked(ctx, lease)
		if err != nil {
			return err
		}
		if owner.Status != execution.StatusRunning || owner.AttemptCount != lease.Identity.DomainAttempt {
			return ErrExecutionState
		}
		bound := completionDecisionMutation{decisionMutation: decisionMutation{u.Effects}, allowed: func(d decision.Decision) bool {
			return d.TenantID == owner.TenantID && d.ScenarioID == owner.ScenarioID && d.ScenarioIterationID == owner.ScenarioIterationID
		}, failed: u.failed}
		if err := persist(bound); err != nil {
			return err
		}
		if *u.failed != nil {
			return *u.failed
		}
		// Rechecks the same generation/attempt AND current lease expiry after
		// every decision/effect write. A late worker rolls back the entire batch.
		return r.Scheduled.Complete(ctx, lease)
	})
}

// CompleteAsync makes the result, optional required callback and prepared
// decision/effect jobs atomic. The source consistency contract still evaluates
// rules/history before this persistence transaction, not under a global DB
// snapshot. Request/item binding belongs to the validated preparation adapter;
// this boundary additionally enforces tenant/scenario/object-type owner scope.
func (s *DecisionUnitOfWorkStore) CompleteAsync(ctx context.Context, lease ExecutionLease, result []byte, persist func(ports.DecisionMutationStore) error) error {
	if persist == nil {
		return errors.New("prepared async-output persistence callback required")
	}
	return s.CompleteAsyncPrepared(ctx, lease, func(store ports.DecisionMutationStore) ([]byte, error) {
		if err := persist(store); err != nil {
			return nil, err
		}
		return result, nil
	})
}

// CompleteAsyncPrepared binds bounded, locally serialized prepared results to
// the actual decision writes on the same transaction. The callback must not
// evaluate AST, call providers or perform network I/O.
func (s *DecisionUnitOfWorkStore) CompleteAsyncPrepared(ctx context.Context, lease ExecutionLease, persist func(ports.DecisionMutationStore) ([]byte, error)) error {
	if persist == nil {
		return errors.New("prepared async-output persistence callback required")
	}
	return s.Run(ctx, ExecutionRun, func(u DecisionUnitOfWork) error {
		r, err := u.Executions()
		if err != nil {
			return err
		}
		owner, err := r.Async.locked(ctx, lease, ports.JobAsync)
		if err != nil {
			return err
		}
		if owner.Status != execution.StatusRunning || owner.AttemptCount != lease.Identity.DomainAttempt {
			return ErrExecutionState
		}
		bound := completionDecisionMutation{decisionMutation: decisionMutation{u.Effects}, allowed: func(d decision.Decision) bool {
			return d.TenantID == owner.TenantID && (owner.ScenarioID == "" || d.ScenarioID == owner.ScenarioID) && d.ObjectType == owner.ObjectType
		}, failed: u.failed}
		result, err := persist(bound)
		if err != nil {
			return err
		}
		if *u.failed != nil {
			return *u.failed
		}
		return r.Async.Complete(ctx, lease, result)
	})
}

type completionDecisionMutation struct {
	decisionMutation
	allowed func(decision.Decision) bool
	failed  *error
}

func (s completionDecisionMutation) Decisions() ports.DecisionRepository {
	return completionDecisionRepository{DecisionRepository: s.decisionMutation.Decisions(), allowed: s.allowed, failed: s.failed}
}

type completionDecisionRepository struct {
	ports.DecisionRepository
	allowed func(decision.Decision) bool
	failed  *error
}

func (r completionDecisionRepository) Create(ctx context.Context, d decision.Decision) (out decision.Decision, err error) {
	defer func() {
		if err != nil && r.failed != nil {
			*r.failed = errors.Join(*r.failed, err)
		}
	}()
	if r.allowed == nil || !r.allowed(d) {
		return out, ErrOwnerRelationshipConflict
	}
	return r.DecisionRepository.Create(ctx, d)
}
