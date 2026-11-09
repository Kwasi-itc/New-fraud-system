package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (s *AuthoringStore) Scope() (uuid.UUID, int, int, error) {
	if s == nil || s.configuration == nil || s.authorize == nil {
		return uuid.Nil, 0, 0, ErrDecisionAuthorizationUnavailable
	}
	c := s.configuration
	return c.tenant, c.maxRows, c.maxBytes, nil
}

// DraftPreparation authorizes the intended write before external model work.
// This read is only a candidate snapshot; CommitPrepared freezes and compares
// it again, including every rule, before changing the draft status.
func (s *AuthoringStore) DraftPreparation(ctx context.Context, parent, iteration uuid.UUID) (out PublicationSnapshot, err error) {
	if s == nil || s.configuration == nil || parent == uuid.Nil || iteration == uuid.Nil {
		return out, ErrTenantScope
	}
	c := s.configuration
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, AuthoringWrite); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		var err error
		out, err = readAuthoringSnapshot(ctx, r, parent, iteration)
		if err == nil && out.Iteration.Status != scenario.IterationStatusDraft {
			return ErrCommittedIteration
		}
		return err
	})
	if err != nil {
		return PublicationSnapshot{}, err
	}
	return out, nil
}

func readAuthoringSnapshot(ctx context.Context, r authoringRepository, parent, iteration uuid.UUID) (out PublicationSnapshot, err error) {
	tenant := r.tenant.String()
	out.Scenario, err = (ScenarioRepository{r}).GetByID(ctx, tenant, parent.String())
	if err != nil {
		return out, err
	}
	out.Iteration, err = (ScenarioIterationRepository{r}).GetByID(ctx, tenant, parent.String(), iteration.String())
	if err != nil {
		return out, err
	}
	out.Rules, err = (RuleRepository{r}).ListByIteration(ctx, tenant, parent.String(), iteration.String())
	return out, err
}

func (s *AuthoringStore) CommitPrepared(ctx context.Context, prepared *PreparedPublicationBinding, at time.Time) (out scenario.Iteration, err error) {
	if s == nil || prepared == nil || prepared.owner != s || prepared.guard == nil || prepared.authorize == nil || prepared.snapshot.Iteration.Status != scenario.IterationStatusDraft {
		return out, ErrTenantScope
	}
	parent, e := uuid.Parse(prepared.snapshot.Scenario.ID)
	iteration, e2 := uuid.Parse(prepared.snapshot.Iteration.ID)
	if e != nil || e2 != nil || parent == uuid.Nil || iteration == uuid.Nil {
		return out, ErrTenantScope
	}
	c := s.configuration
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, AuthoringWrite); err != nil {
			return err
		}
		if err := prepared.authorize(ctx, tx); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, true}}
		var found mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+c.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR SHARE", mysqlstore.UUID(c.tenant), mysqlstore.UUID(parent)).Scan(&found); err != nil {
			return err
		}
		// All rule mutations hold SHARE on this iteration; UPDATE freezes the
		// entire rule set without locking unrelated scenarios or a tenant row.
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+c.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=? FOR UPDATE", mysqlstore.UUID(c.tenant), mysqlstore.UUID(parent), mysqlstore.UUID(iteration)).Scan(&found); err != nil {
			return err
		}
		current, err := readAuthoringSnapshot(ctx, r, parent, iteration)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if sha256.Sum256(raw) != prepared.digest {
			return ErrExecutionBindingChanged
		}
		if err := prepared.guard(ctx, tx); err != nil {
			return err
		}
		out, err = (ScenarioIterationRepository{r}).Commit(ctx, c.tenant.String(), parent.String(), iteration.String(), at)
		return err
	})
	if err != nil {
		return scenario.Iteration{}, err
	}
	return out, nil
}

// SaveScenarioWithModel validates the trigger's current owner registry on the
// same transaction as the metadata change. Model retrieval occurs before this
// method; no provider calls are made while holding database locks.
func (s *AuthoringStore) SaveScenarioWithModel(ctx context.Context, reader TenantDataReader, model ports.TenantModel, value scenario.Scenario, create bool) (out scenario.Scenario, err error) {
	if !reader.MatchesPublicationScope(s) || value.TenantID != s.configuration.tenant.String() {
		return out, ErrTenantScope
	}
	guard, err := reader.executionRegistryGuard(map[string]bool{value.TriggerObjectType: true}, nil, model)
	if err != nil {
		return out, err
	}
	c := s.configuration
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, AuthoringWrite); err != nil {
			return err
		}
		if err := reader.configuration.authorize(ctx, tx, c.tenant, false); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, true}}
		repo := ScenarioRepository{r}
		if !create {
			args, err := r.ids(value.TenantID, value.ID)
			if err != nil {
				return err
			}
			var found mysqlstore.UUID
			if err := tx.QueryRowContext(ctx, "SELECT id FROM "+c.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
				return err
			}
			current, err := repo.GetByID(ctx, value.TenantID, value.ID)
			if err != nil {
				return err
			}
			value.CreatedAt, value.LiveIterationID = current.CreatedAt, current.LiveIterationID
		}
		if err := guard(ctx, tx); err != nil {
			return err
		}
		if create {
			out, err = repo.Create(ctx, value)
		} else {
			out, err = repo.Update(ctx, value)
		}
		return err
	})
	if err != nil {
		return scenario.Scenario{}, err
	}
	return out, nil
}
