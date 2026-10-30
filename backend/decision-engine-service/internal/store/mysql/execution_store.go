package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrExecutionState = errors.New("execution state or expected attempt/generation does not match")
var ErrExecutionReplayConflict = errors.New("execution identity and idempotency/occurrence keys refer to different requests")
var ErrExecutionLease = errors.New("execution delivery lease is stale, expired or outside this tenant")

type ExecutionAction string

const (
	ExecutionRead          ExecutionAction = "decision.execution.read"
	ExecutionRequest       ExecutionAction = "decision.execution.request"
	ExecutionRun           ExecutionAction = "decision.execution.run"
	ExecutionRescue        ExecutionAction = "decision.execution.rescue"
	ExecutionCancel        ExecutionAction = "decision.execution.cancel"
	ExecutionRedrive       ExecutionAction = "decision.execution.redrive"
	ExecutionScheduleAdmin ExecutionAction = "decision.execution.schedule.author"
	ExecutionScheduleTick  ExecutionAction = "decision.execution.schedule.tick"
)

// ExecutionAuthorizer validates a trusted user/service and this exact action,
// on the caller transaction before owner locks. Run/tick require service scope;
// redrive/admin must not inherit generic persistence permission. No real auth
// provider is supplied here. No network calls, DDL, commit or retained tx.
type ExecutionAuthorizer func(context.Context, *sql.Tx, uuid.UUID, ExecutionAction) error

type ExecutionPolicy struct {
	Queues              map[ports.JobKind]DeliveryQueuePolicy
	CallbackMaxAttempts int
}

// These records expose the new owner's generation explicitly. They are not an
// unsafe implementation of legacy mutation ports which lack attempt/lease fences.
type ScheduledExecutionRecord struct {
	execution.ScheduledExecution
	Generation  int
	CancelledAt *time.Time
}
type AsyncExecutionRecord struct {
	execution.AsyncDecisionExecution
	Generation, CallbackMaxAttempts  int
	CancelledAt, CallbackCancelledAt *time.Time
}
type ScheduledCreation struct {
	Record ScheduledExecutionRecord
	Replay bool
}
type AsyncCreation struct {
	Record AsyncExecutionRecord
	Replay bool
}

type ExecutionLease struct {
	ID, Token        uuid.UUID
	Identity         mysqljobs.Identity
	TransportAttempt int
}

type ExecutionStore struct {
	configuration *PlatformStore
	authorize     ExecutionAuthorizer
	policy        ExecutionPolicy
}
type ExecutionRepositories struct {
	Scheduled ScheduledExecutionRepository
	Async     AsyncDecisionExecutionRepository
	Schedules ExecutionScheduleRepository
}
type executionRepository struct {
	authoringRepository
	action ExecutionAction
	policy ExecutionPolicy
	failed *error
}

func validExecutionAction(a ExecutionAction) bool {
	switch a {
	case ExecutionRead, ExecutionRequest, ExecutionRun, ExecutionRescue, ExecutionCancel, ExecutionRedrive, ExecutionScheduleAdmin, ExecutionScheduleTick:
		return true
	}
	return false
}

func NewExecutionStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, authorize ExecutionAuthorizer, policy ExecutionPolicy) (*ExecutionStore, error) {
	if authorize == nil || len(policy.Queues) != 3 || policy.CallbackMaxAttempts < 1 || policy.CallbackMaxAttempts > math.MaxInt32 {
		return nil, errors.New("execution authorizer, all three queues and positive signed callback limit required")
	}
	copyPolicy := ExecutionPolicy{Queues: map[ports.JobKind]DeliveryQueuePolicy{}, CallbackMaxAttempts: policy.CallbackMaxAttempts}
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		p, ok := policy.Queues[kind]
		if !ok || !effectQueueName.MatchString(p.Name) || p.MaxAttempts < 1 || p.MaxAttempts > math.MaxInt32 {
			return nil, errors.New("valid explicit execution transport policies required")
		}
		copyPolicy.Queues[kind] = p
	}
	c, err := NewPlatformStore(db, database, tenant, maxRows, maxBytes, func(context.Context, *sql.Tx, uuid.UUID, bool) error {
		return errors.New("execution action authorization is required")
	})
	if err != nil {
		return nil, err
	}
	return &ExecutionStore{c, authorize, copyPolicy}, nil
}

// Run is a short local-SQL unit of work. Results are provisional until commit.
// No automatic retries: CommitError requires reconciliation by original ID/key.
// Repositories are serial callback-local values, never retained or used over I/O.
func (s *ExecutionStore) Run(ctx context.Context, action ExecutionAction, fn func(ExecutionRepositories) error) error {
	if s == nil || s.configuration == nil || s.authorize == nil || fn == nil || !validExecutionAction(action) {
		return errors.New("initialized execution store, exact action and callback required")
	}
	c := s.configuration
	run := mysqlstore.Mutation
	if action == ExecutionRead {
		run = mysqlstore.Snapshot
	}
	return run(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, action); err != nil {
			return err
		}
		var failed error
		err := fn(s.repositories(tx, action, &failed))
		return errors.Join(err, failed)
	})
}

func (s *ExecutionStore) repositories(tx *sql.Tx, action ExecutionAction, failed *error) ExecutionRepositories {
	c := s.configuration
	r := executionRepository{authoringRepository: authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, action != ExecutionRead}}, action: action, policy: s.policy, failed: failed}
	return ExecutionRepositories{ScheduledExecutionRepository{r}, AsyncDecisionExecutionRepository{r}, ExecutionScheduleRepository{r}}
}

func (r executionRepository) require(actions ...ExecutionAction) error {
	if err := r.mutating(); err != nil {
		return err
	}
	for _, action := range actions {
		if r.action == action {
			return nil
		}
	}
	return ErrDecisionAuthorizationDenied
}
func (r executionRepository) poison(err *error) {
	if *err != nil && r.failed != nil {
		*r.failed = errors.Join(*r.failed, *err)
	}
}

func executionOwner(kind ports.JobKind) string {
	switch kind {
	case ports.JobScheduled:
		return "decision_engine_scheduled_executions"
	case ports.JobAsync, ports.JobCallback:
		return "decision_engine_async_executions"
	}
	return ""
}
