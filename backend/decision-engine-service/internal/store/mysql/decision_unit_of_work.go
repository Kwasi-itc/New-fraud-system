package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// DecisionUnitOfWorkStore composes the implemented decision/effect/list and
// execution owners on ONE shared-database transaction. It is not a partial
// legacy ports.TransactionManager: unfenced execution mutations are deliberately
// absent. All identity providers must use consistent authorization lock ordering
// before any owner lock; callbacks are serial, local SQL only and never retained.
// Captured values are provisional until the enclosing call returns nil.
type DecisionUnitOfWorkStore struct {
	effects    *EffectStore
	executions *ExecutionStore
}

type DecisionUnitOfWork struct {
	Effects    EffectRepositories
	executions *ExecutionRepositories
	failed     *error
}

var _ ports.DecisionTransactionManager = (*DecisionUnitOfWorkStore)(nil)

func NewDecisionUnitOfWorkStore(effects *EffectStore, executions *ExecutionStore) (*DecisionUnitOfWorkStore, error) {
	if effects == nil || effects.configuration == nil || executions == nil || executions.configuration == nil || executions.authorize == nil {
		return nil, errors.New("queued effects and fenced execution stores required")
	}
	a, b := effects.configuration, executions.configuration
	if a.db == nil || a.authorize == nil || a.db != b.db || a.schema != b.schema || a.tenant != b.tenant || a.maxRows != b.maxRows || a.maxBytes != b.maxBytes {
		return nil, errors.New("unit of work requires identical pool/schema/tenant/row/byte scope")
	}
	if _, err := copyEffectQueues(effects.queues); err != nil {
		return nil, err
	}
	// Inputs are immutable configured stores; no callback can replace a provider,
	// tenant, queue policy or pool while this unit of work is running.
	e, x := *effects, *executions
	return &DecisionUnitOfWorkStore{&e, &x}, nil
}

// Executions is an explicitly requested capability. Ignoring a missing
// capability error still poisons this unit of work, rather than committing
// otherwise successful decision writes without the requested lifecycle work.
func (u DecisionUnitOfWork) Executions() (ExecutionRepositories, error) {
	if u.executions == nil {
		if u.failed != nil {
			*u.failed = errors.Join(*u.failed, ErrDecisionAuthorizationUnavailable)
		}
		return ExecutionRepositories{}, ErrDecisionAuthorizationUnavailable
	}
	return *u.executions, nil
}

// DecisionMutations is a callback-local capability on this same transaction,
// used for prepared lifecycle events alongside owner state transitions. Never
// retain it or perform provider/network work while this unit of work is open.
func (u DecisionUnitOfWork) DecisionMutations() ports.DecisionMutationStore {
	if u.failed == nil {
		return nil
	}
	return decisionMutation{u.Effects}
}

// Run grants only the named execution action, or no execution capability for
// an empty action. Read-only execution work belongs to ExecutionStore.Run(Read),
// not a writable mixed transaction. Required jobs are intrinsic owner mutations.
func (s *DecisionUnitOfWorkStore) Run(ctx context.Context, action ExecutionAction, fn func(DecisionUnitOfWork) error) error {
	if s == nil || s.effects == nil || s.executions == nil || fn == nil || action == ExecutionRead || action != "" && !validExecutionAction(action) {
		return errors.New("initialized unit of work, explicit write action and callback required")
	}
	c := s.effects.configuration
	timings, hasTimings := ports.TransactionTimingsFromContext(ctx)
	started := time.Now()
	var commitStarted time.Time
	result := mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		bodyStarted := time.Now()
		if hasTimings {
			timings.BeginMicros = bodyStarted.Sub(started).Microseconds()
			defer func() { timings.BodyMicros = time.Since(bodyStarted).Microseconds() }()
		}
		if err := c.authorize(ctx, tx, c.tenant, true); err != nil {
			return err
		}
		lists, err := s.effects.authorizedCustomLists(ctx, tx, true)
		if err != nil {
			return err
		}
		if action != "" {
			if err := s.executions.authorize(ctx, tx, c.tenant, action); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var failed error
		u := DecisionUnitOfWork{Effects: s.effects.repositories(tx, true, lists, &failed), failed: &failed}
		if action != "" {
			r := s.executions.repositories(tx, action, &failed)
			u.executions = &r
		}
		err = fn(u)
		err = errors.Join(err, failed)
		if err == nil {
			commitStarted = time.Now()
		}
		return err
	})
	var uncertain *mysqlstore.CommitError
	if hasTimings && !commitStarted.IsZero() && (result == nil || errors.As(result, &uncertain)) {
		timings.CommitMicros = time.Since(commitStarted).Microseconds()
	}
	return result
}

func (s *DecisionUnitOfWorkStore) RunDecisionMutation(ctx context.Context, fn func(ports.DecisionMutationStore) error) error {
	if fn == nil {
		return errors.New("decision persistence callback required")
	}
	return s.Run(ctx, "", func(u DecisionUnitOfWork) error { return fn(decisionMutation{u.Effects}) })
}

type decisionMutation struct{ effects EffectRepositories }

func (s decisionMutation) Decisions() ports.DecisionRepository { return s.effects.Decisions }
func (s decisionMutation) RuleExecutions() ports.RuleExecutionRepository {
	return s.effects.RuleExecutions
}
func (s decisionMutation) WorkflowExecutions() ports.WorkflowExecutionRepository {
	return s.effects.WorkflowExecutions
}
func (s decisionMutation) ScreeningExecutions() ports.ScreeningExecutionRepository {
	return s.effects.ScreeningExecutions
}
func (s decisionMutation) ScoringRequests() ports.ScoringRequestRepository {
	return s.effects.ScoringRequests
}
func (s decisionMutation) OutboxEvents() ports.OutboxEventRepository { return s.effects.OutboxEvents }

func (s decisionMutation) Enqueue(ctx context.Context, job ports.JobRequest) (err error) {
	r := s.effects.Decisions.authoringRepository
	defer func() {
		if err != nil && r.effectJobError != nil {
			*r.effectJobError = errors.Join(*r.effectJobError, err)
		}
	}()
	if job.ScheduledAt != nil || job.TenantID != r.tenant.String() || effectOwnerTable(job.Kind) == "" {
		return errors.New("decision persistence requires same-tenant four-effect initial jobs; execution jobs require their fenced lifecycle")
	}
	// CreateMany already schedules its required jobs in bounded batches. This
	// source-compatible call rechecks owner state and the exact permanent receipt;
	// it never bypasses scheduling or invents an unbound arbitrary job payload.
	return r.enqueueCreated(ctx, effectOwnerTable(job.Kind), []string{job.ResourceID})
}
