package mysqlexecution

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ScoringAuthoring owns only configuration. It neither sends scoring requests
// nor supplies authority or provider defaults. Each operation reauthorizes on
// the owner transaction and returns no provisional value after rollback.
type ScoringAuthoring struct {
	owner             *mysql.ConfigurationStore
	tenant            uuid.UUID
	ids               ports.IDGenerator
	clock             ports.Clock
	maxRows, maxBytes int
}

func NewScoringAuthoring(owner *mysql.ConfigurationStore, ids ports.IDGenerator, clock ports.Clock) (*ScoringAuthoring, error) {
	if ids == nil || clock == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	tenant, rows, bytes, err := owner.Scope()
	if err != nil {
		return nil, err
	}
	return &ScoringAuthoring{owner, tenant, ids, clock, rows, bytes}, nil
}

func (s *ScoringAuthoring) MatchesExecutionStore(owner *mysql.ExecutionStore) bool {
	return s != nil && s.owner.MatchesExecutionStore(owner)
}

func scoringRun[T any](ctx context.Context, s *ScoringAuthoring, write bool, fn func(mysql.ConfigurationRepositories) (T, error)) (out T, err error) {
	if s == nil || s.owner == nil {
		return out, mysql.ErrOwnerContractUnavailable
	}
	run := s.owner.ReadSnapshot
	if write {
		run = s.owner.Mutate
	}
	err = run(ctx, func(r mysql.ConfigurationRepositories) error { out, err = fn(r); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

func (s *ScoringAuthoring) List(ctx context.Context, parent uuid.UUID) ([]scoring.Config, error) {
	return scoringRun(ctx, s, false, func(r mysql.ConfigurationRepositories) ([]scoring.Config, error) {
		if _, err := r.Scenarios.GetByID(ctx, s.tenant.String(), parent.String()); err != nil {
			return nil, err
		}
		return r.ScoringConfigs.ListByScenario(ctx, s.tenant.String(), parent.String())
	})
}

func (s *ScoringAuthoring) Get(ctx context.Context, parent, id uuid.UUID) (scoring.Config, error) {
	return scoringRun(ctx, s, false, func(r mysql.ConfigurationRepositories) (scoring.Config, error) {
		return r.ScoringConfigs.GetByID(ctx, s.tenant.String(), parent.String(), id.String())
	})
}

func (s *ScoringAuthoring) Save(ctx context.Context, parent, id uuid.UUID, fields scoring.Config) (scoring.Config, error) {
	return scoringRun(ctx, s, true, func(r mysql.ConfigurationRepositories) (scoring.Config, error) {
		create := id == uuid.Nil
		var current scoring.Config
		if create {
			if _, err := r.Scenarios.GetByID(ctx, s.tenant.String(), parent.String()); err != nil {
				return current, err
			}
			id = s.ids.New()
			if id == uuid.Nil {
				return current, mysql.ErrTenantScope
			}
			current = scoring.Config{ID: id.String(), TenantID: s.tenant.String(), ScenarioID: parent.String(), CreatedAt: s.clock.Now().UTC().Truncate(time.Microsecond)}
		} else {
			var err error
			current, err = r.ScoringConfigs.GetByID(ctx, s.tenant.String(), parent.String(), id.String())
			if err != nil {
				return scoring.Config{}, err
			}
		}
		current.Name, current.RulesetRef, current.ConfigJSON, current.Active, current.AllowedOutcomes = fields.Name, fields.RulesetRef, fields.ConfigJSON, fields.Active, fields.AllowedOutcomes
		current.UpdatedAt = s.clock.Now().UTC().Truncate(time.Microsecond)
		if err := current.Validate(); err != nil {
			return scoring.Config{}, errors.Join(service.ErrExecutionPreparation, err)
		}
		if !json.Valid(current.ConfigJSON) {
			return scoring.Config{}, service.ErrExecutionPreparation
		}
		if len(current.AllowedOutcomes) > s.maxRows {
			return scoring.Config{}, mysql.ErrProjectionLimit
		}
		used := len(current.ConfigJSON)
		for _, value := range append([]string{current.Name, current.RulesetRef}, current.AllowedOutcomes...) {
			if _, err := (mysqlstore.Text(value)).Value(); err != nil {
				return scoring.Config{}, errors.Join(service.ErrExecutionPreparation, err)
			}
			if len(value) > s.maxBytes-used {
				return scoring.Config{}, mysql.ErrProjectionLimit
			}
			used += len(value)
		}
		if create {
			return r.ScoringConfigs.Create(ctx, current)
		}
		return r.ScoringConfigs.Update(ctx, current)
	})
}

func (s *ScoringAuthoring) Delete(ctx context.Context, parent, id uuid.UUID) error {
	_, err := scoringRun(ctx, s, true, func(r mysql.ConfigurationRepositories) (struct{}, error) {
		return struct{}{}, r.ScoringConfigs.Delete(ctx, s.tenant.String(), parent.String(), id.String())
	})
	return err
}
