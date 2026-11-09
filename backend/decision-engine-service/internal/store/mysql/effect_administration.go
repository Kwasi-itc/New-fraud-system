package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type EffectAdministration struct {
	owner    *EffectStore
	database string
	clock    ports.Clock
	recovery EffectRetryAuthorizer
	policies map[ports.JobKind]EffectQueuePolicy
}

func NewEffectAdministration(db *sql.DB, database string, tenant uuid.UUID, rows, bytes int, authorize PlatformAuthorizer, recovery EffectRetryAuthorizer, source *ScreeningSourceContract, policies map[ports.JobKind]EffectQueuePolicy, clock ports.Clock) (*EffectAdministration, error) {
	if authorize == nil || recovery == nil || clock == nil {
		return nil, ErrOwnerContractUnavailable
	}
	owner, err := NewEffectStore(db, database, tenant, rows, bytes, authorize, source)
	if err != nil {
		return nil, err
	}
	copyPolicies := map[ports.JobKind]EffectQueuePolicy{}
	for kind, policy := range policies {
		copyPolicies[kind] = policy
	}
	return &EffectAdministration{owner, database, clock, recovery, copyPolicies}, nil
}
func (s *EffectAdministration) MatchesExecutionStore(owner *ExecutionStore) bool {
	return s != nil && owner != nil && sameEvaluationScope(s.owner.configuration, owner.configuration)
}
func (s *EffectAdministration) Screenings(ctx context.Context, decision uuid.UUID) (out []screening.Execution, err error) {
	err = s.owner.ReadSnapshot(ctx, func(r EffectRepositories) error {
		if _, err := r.Decisions.GetByID(ctx, r.Decisions.tenant.String(), decision.String()); err != nil {
			return err
		}
		out, err = r.ScreeningExecutions.ListByDecision(ctx, r.Decisions.tenant.String(), decision.String())
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *EffectAdministration) Screening(ctx context.Context, id uuid.UUID) (out screening.Execution, err error) {
	err = s.owner.ReadSnapshot(ctx, func(r EffectRepositories) error {
		out, err = r.ScreeningExecutions.GetByID(ctx, r.Decisions.tenant.String(), id.String())
		return err
	})
	return out, err
}
func (s *EffectAdministration) Scoring(ctx context.Context, id uuid.UUID) (out scoring.Request, err error) {
	err = s.owner.ReadSnapshot(ctx, func(r EffectRepositories) error {
		out, err = r.ScoringRequests.GetByID(ctx, r.Decisions.tenant.String(), id.String())
		return err
	})
	return out, err
}

type EffectStatusInput struct {
	Status            string
	ProviderReference *string
	ResponseJSON      *json.RawMessage
	LastError         *string
}

func (s *EffectAdministration) UpdateStatus(ctx context.Context, id uuid.UUID, kind ports.JobKind, input EffectStatusInput) (any, error) {
	if !validProviderStatus(input.Status) || kind != ports.JobScreening && kind != ports.JobScoring {
		return nil, ErrAdministrationInput
	}
	var out any
	err := s.owner.Mutate(ctx, func(r EffectRepositories) error {
		t := r.Decisions.tenant.String()
		now := s.clock.Now()
		// Lock before reading optional values so two partial updates cannot lose
		// each other's fields. Source-owned screenings remain owner-reconciled.
		repo := r.ScoringRequests.providerExecutionRepository
		if kind == ports.JobScreening {
			repo = r.ScreeningExecutions.providerExecutionRepository
		}
		var found mysqlstore.UUID
		where := " WHERE tenant_id=? AND id=?"
		if kind == ports.JobScreening {
			where += " AND projection_source_id IS NULL"
		}
		if err := repo.tx.QueryRowContext(ctx, "SELECT id FROM "+repo.schema+"."+repo.table()+where+" FOR UPDATE", mysqlstore.UUID(repo.tenant), mysqlstore.UUID(id)).Scan(&found); err != nil {
			return err
		}
		item, err := repo.get(ctx, t, id.String())
		if err != nil {
			return err
		}
		item.status, item.updated = input.Status, now
		if input.ProviderReference != nil {
			item.reference = *input.ProviderReference
		}
		if input.ResponseJSON != nil {
			item.response = mysqlstore.JSON(*input.ResponseJSON)
		}
		if input.LastError != nil {
			item.lastError = *input.LastError
		}
		switch input.Status {
		case "sent":
			item.sent = &now
			item.failed = nil
		case "completed":
			item.completed = &now
			item.failed = nil
			item.lastError = ""
		case "failed":
			item.failed = &now
			item.completed = nil
		case "queued", "pending":
			item.completed = nil
			item.failed = nil
		}
		stored, err := repo.update(ctx, item)
		if err != nil {
			return err
		}
		if kind == ports.JobScreening {
			out = screeningExecution(stored)
		} else {
			out = scoringRequest(stored)
		}
		return nil
	})
	return out, err
}
func (s *EffectAdministration) Retry(ctx context.Context, id uuid.UUID, kind ports.JobKind) error {
	if kind != ports.JobScreening && kind != ports.JobScoring {
		return ErrAdministrationInput
	}
	c := s.owner.configuration
	identity := mysqljobs.Identity{Version: 1, TenantID: c.tenant, ResourceID: id, Kind: kind}
	err := mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.recovery(ctx, tx, c.tenant, kind, EffectRedrive); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT domain_attempt,redrive_generation FROM "+c.schema+".decision_engine_effect_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? ORDER BY domain_attempt DESC,redrive_generation DESC LIMIT 1", mysqlstore.UUID(c.tenant), string(kind), mysqlstore.UUID(id)).Scan(&identity.DomainAttempt, &identity.RedriveGeneration)
	})
	if err != nil {
		return err
	}
	retry, err := NewEffectRetryStore(c.db, s.database, c.tenant, kind, s.policies[kind], s.policies[kind].MaxAttempts, s.recovery)
	if err != nil {
		return err
	}
	_, err = retry.Redrive(ctx, identity)
	return err
}
func (s *EffectAdministration) ReconcileScreening(ctx context.Context, tenant, source, decision, config, expectedScenario uuid.UUID) (out screening.Execution, err error) {
	if s == nil || tenant != s.owner.configuration.tenant {
		return out, ErrDecisionAuthorizationDenied
	}
	err = s.owner.Mutate(ctx, func(r EffectRepositories) error {
		out, err = r.ScreeningExecutions.ReconcileSource(ctx, tenant.String(), source.String(), decision.String(), config.String())
		if err == nil && expectedScenario != uuid.Nil && out.ScenarioID != expectedScenario.String() {
			return ErrOwnerRelationshipConflict
		}
		return err
	})
	return out, err
}
