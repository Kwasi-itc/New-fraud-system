package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrExecutionBindingChanged = errors.New("execution admission scenario binding changed; prepare again")

func (s *DecisionUnitOfWorkStore) MatchesExecutionPreparation(owner *ExecutionStore) bool {
	if s == nil || s.executions == nil || owner == nil || owner.configuration == nil || s.executions.configuration == nil {
		return false
	}
	a, b := s.executions.configuration, owner.configuration
	if a.db != b.db || a.schema != b.schema || a.tenant != b.tenant || a.maxRows != b.maxRows || a.maxBytes != b.maxBytes || s.executions.policy.CallbackMaxAttempts != owner.policy.CallbackMaxAttempts {
		return false
	}
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		if s.executions.policy.Queues[kind] != owner.policy.Queues[kind] {
			return false
		}
	}
	return true
}

func (r executionRepository) replayScheduledKey(ctx context.Context, key string) (*ScheduledExecutionRecord, error) {
	if err := r.scope(r.tenant.String(), key); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, nil
	}
	repo := ScheduledExecutionRepository{r}
	v, err := platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, repo.selectSQL()+" WHERE e.tenant_id=? AND e.idempotency_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(e.idempotency_key AS BINARY)=CAST(? AS BINARY)", mysqlstore.UUID(r.tenant), mysqlstore.Text(key), mysqlstore.Text(key)), repo.scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func (r executionRepository) replayAsyncKey(ctx context.Context, key string) (*AsyncExecutionRecord, error) {
	if err := r.scope(r.tenant.String(), key); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, nil
	}
	repo := AsyncDecisionExecutionRepository{r}
	v, err := platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, repo.selectSQL()+" WHERE e.tenant_id=? AND e.idempotency_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(e.idempotency_key AS BINARY)=CAST(? AS BINARY)", mysqlstore.UUID(r.tenant), mysqlstore.Text(key), mysqlstore.Text(key)), repo.scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// Replay lookups reauthorize request authority even though they do not write.
// They let a committed original request survive later model/publication changes;
// replay never admits a second job or emits a duplicate queued lifecycle event.
func (s *ExecutionStore) ReplayScheduledRequest(ctx context.Context, tenant, parent, key string) (out *ScheduledCreation, err error) {
	err = s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
		if _, err := r.Scheduled.ids(tenant, parent); err != nil {
			return err
		}
		v, err := r.Scheduled.replayScheduledKey(ctx, key)
		if err != nil {
			return err
		}
		if v != nil {
			if v.ScenarioID != parent {
				return ErrExecutionReplayConflict
			}
			out = &ScheduledCreation{Record: *v, Replay: true}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *ExecutionStore) ReplayAsyncRequest(ctx context.Context, tenant, parent, object, key string) (out *AsyncCreation, err error) {
	err = s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
		if err := r.Async.scope(tenant); err != nil {
			return err
		}
		v, err := r.Async.replayAsyncKey(ctx, key)
		if err != nil {
			return err
		}
		if v != nil {
			if v.ScenarioID != parent || v.ObjectType != object {
				return ErrExecutionReplayConflict
			}
			out = &AsyncCreation{Record: *v, Replay: true}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r executionRepository) lockAdmissionBinding(ctx context.Context, snapshot ExecutionPreparationSnapshot) error {
	if snapshot.parent.TenantID != r.tenant.String() || snapshot.iteration.TenantID != r.tenant.String() || snapshot.iteration.ScenarioID != snapshot.parent.ID {
		return ErrTenantScope
	}
	var id mysqlstore.UUID
	err := r.tx.QueryRowContext(ctx, "SELECT s.id FROM "+r.schema+".decision_engine_scenarios s JOIN "+r.schema+".decision_engine_scenario_live_iterations l ON l.tenant_id=s.tenant_id AND l.scenario_id=s.id WHERE s.tenant_id=? AND s.id=? AND l.iteration_id=? AND CAST(s.trigger_object_type AS BINARY)=CAST(? AS BINARY) FOR SHARE OF s,l", mysqlstore.UUID(r.tenant), mysqlstore.UUID(uuid.MustParse(snapshot.parent.ID)), mysqlstore.UUID(uuid.MustParse(snapshot.iteration.ID)), mysqlstore.Text(snapshot.parent.TriggerObjectType)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExecutionBindingChanged
	}
	return err
}

func (r TenantDataReader) admissionGuard(ctx context.Context, snapshot *ExecutionPreparationSnapshot, model ports.TenantModel, object string) (func(context.Context, *sql.Tx) error, error) {
	queries := []ports.AggregateQuery{}
	if snapshot != nil {
		var err error
		queries, err = aggregatePublicationQueries(ctx, PublicationSnapshot{snapshot.parent, snapshot.iteration, snapshot.rules}, model, r.configuration.maxRows, r.configuration.maxBytes)
		if err != nil {
			return nil, err
		}
	}
	return r.executionRegistryGuard(map[string]bool{object: true}, queries, model)
}

// AdmitScheduled rechecks trusted authority BEFORE owner/registry locks. Model
// provider IO, payload validation and AST template preparation already happened.
// Parent/live link -> registered table gates -> owner/jobs/event is the lock
// order. No external callback or physical DDL is performed in this transaction.
func (s *DecisionUnitOfWorkStore) AdmitScheduled(ctx context.Context, reader TenantDataReader, snapshot ExecutionPreparationSnapshot, model ports.TenantModel, input execution.ScheduledExecution, lifecycle func(ports.DecisionMutationStore) error) (out ScheduledCreation, err error) {
	if s == nil || s.executions == nil || !reader.MatchesExecutionPreparationScope(s.executions) || lifecycle == nil || input.ScenarioID != snapshot.parent.ID || input.ScenarioIterationID != snapshot.iteration.ID {
		return out, ErrExecutionBindingChanged
	}
	guard, err := reader.admissionGuard(ctx, &snapshot, model, snapshot.parent.TriggerObjectType)
	if err != nil {
		return out, err
	}
	err = s.Run(ctx, ExecutionRequest, func(u DecisionUnitOfWork) error {
		r, err := u.Executions()
		if err != nil {
			return err
		}
		if err := r.Scheduled.scope(input.TenantID); err != nil {
			return err
		}
		prior, err := r.Scheduled.replayScheduledKey(ctx, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.ScenarioID != input.ScenarioID {
				return ErrExecutionReplayConflict
			}
			out = ScheduledCreation{Record: *prior, Replay: true}
			return nil
		}
		c := reader.configuration
		if err := c.authorize(ctx, r.Scheduled.tx, c.tenant, false); err != nil {
			return err
		}
		if err := r.Scheduled.lockAdmissionBinding(ctx, snapshot); err != nil {
			return err
		}
		if err := guard(ctx, r.Scheduled.tx); err != nil {
			return err
		}
		out, err = r.Scheduled.Create(ctx, input)
		if err != nil {
			return err
		}
		if out.Replay {
			return nil
		}
		return lifecycle(u.DecisionMutations())
	})
	if err != nil {
		return ScheduledCreation{}, err
	}
	return out, nil
}
func (s *DecisionUnitOfWorkStore) AdmitAsync(ctx context.Context, reader TenantDataReader, snapshot *ExecutionPreparationSnapshot, model ports.TenantModel, input execution.AsyncDecisionExecution, lifecycle func(ports.DecisionMutationStore) error) (out AsyncCreation, err error) {
	if s == nil || s.executions == nil || !reader.MatchesExecutionPreparationScope(s.executions) || lifecycle == nil || (snapshot == nil) != (input.ScenarioID == "") {
		return out, ErrExecutionBindingChanged
	}
	if snapshot != nil && (input.ScenarioID != snapshot.parent.ID || input.ObjectType != snapshot.parent.TriggerObjectType) {
		return out, ErrExecutionBindingChanged
	}
	guard, err := reader.admissionGuard(ctx, snapshot, model, input.ObjectType)
	if err != nil {
		return out, err
	}
	err = s.Run(ctx, ExecutionRequest, func(u DecisionUnitOfWork) error {
		r, err := u.Executions()
		if err != nil {
			return err
		}
		if err := r.Async.scope(input.TenantID); err != nil {
			return err
		}
		prior, err := r.Async.replayAsyncKey(ctx, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.ScenarioID != input.ScenarioID || prior.ObjectType != input.ObjectType {
				return ErrExecutionReplayConflict
			}
			out = AsyncCreation{Record: *prior, Replay: true}
			return nil
		}
		c := reader.configuration
		if err := c.authorize(ctx, r.Async.tx, c.tenant, false); err != nil {
			return err
		}
		if snapshot != nil {
			if err := r.Async.lockAdmissionBinding(ctx, *snapshot); err != nil {
				return err
			}
		}
		if err := guard(ctx, r.Async.tx); err != nil {
			return err
		}
		out, err = r.Async.Create(ctx, input)
		if err != nil {
			return err
		}
		if out.Replay {
			return nil
		}
		return lifecycle(u.DecisionMutations())
	})
	if err != nil {
		return AsyncCreation{}, err
	}
	return out, nil
}
