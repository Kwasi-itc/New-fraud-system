package mysql

import (
	"context"
	"database/sql"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PreparationForDirect authenticates the request before selecting bounded live
// metadata. It does not allocate a queue resource or retain a SQL transaction.
func (s *ExecutionStore) PreparationForDirect(ctx context.Context, parent uuid.UUID, object string) (out []ExecutionPreparationSnapshot, err error) {
	if s == nil || s.configuration == nil || s.authorize == nil {
		return nil, ErrDecisionAuthorizationUnavailable
	}
	c := s.configuration
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, ExecutionRequest); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		var parents []scenario.Scenario
		if parent != uuid.Nil {
			item, err := (ScenarioRepository{r}).GetByID(ctx, c.tenant.String(), parent.String())
			if err != nil {
				return err
			}
			parents = []scenario.Scenario{item}
		} else {
			var err error
			parents, err = (ScenarioRepository{r}).ListLiveByTriggerObject(ctx, c.tenant.String(), object)
			if err != nil {
				return err
			}
		}
		var err error
		out, err = r.livePreparationSnapshots(ctx, parents, object, 0)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PersistDirect rechecks live selection, current authorization and registry
// identity in the same transaction as every decision/effect. Evaluating AST or
// consulting model/provider services must finish before entering this method.
func (s *DecisionUnitOfWorkStore) PersistDirect(ctx context.Context, reader TenantDataReader, snapshots []ExecutionPreparationSnapshot, model ports.TenantModel, object string, persist func(ports.DecisionMutationStore) error) error {
	if s == nil || !reader.MatchesExecutionPreparationScope(s.executions) || persist == nil {
		return ErrDecisionAuthorizationUnavailable
	}
	if len(snapshots) > reader.configuration.maxRows {
		return ErrProjectionLimit
	}
	queries := []ports.AggregateQuery{}
	for _, snapshot := range snapshots {
		q, err := aggregatePublicationQueries(ctx, PublicationSnapshot{snapshot.parent, snapshot.iteration, snapshot.rules}, model, reader.configuration.maxRows, reader.configuration.maxBytes)
		if err != nil {
			return err
		}
		queries = append(queries, q...)
		if len(queries) > reader.configuration.maxRows {
			return ErrProjectionLimit
		}
	}
	guard, err := reader.executionRegistryGuard(map[string]bool{object: true}, queries, model)
	if err != nil {
		return err
	}
	ordered := append([]ExecutionPreparationSnapshot(nil), snapshots...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].parent.ID < ordered[j].parent.ID })
	return s.Run(ctx, ExecutionRequest, func(u DecisionUnitOfWork) error {
		r, err := u.Executions()
		if err != nil {
			return err
		}
		c := reader.configuration
		if err := c.authorize(ctx, r.Scheduled.tx, c.tenant, false); err != nil {
			return err
		}
		for _, snapshot := range ordered {
			if err := r.Scheduled.lockAdmissionBinding(ctx, snapshot); err != nil {
				return err
			}
		}
		if err := guard(ctx, r.Scheduled.tx); err != nil {
			return err
		}
		return persist(u.DecisionMutations())
	})
}
