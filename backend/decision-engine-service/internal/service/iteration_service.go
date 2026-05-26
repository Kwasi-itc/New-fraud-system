package service

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type IterationService struct {
	txManager ports.TransactionManager
	idGen     ports.IDGenerator
	clock     ports.Clock
	readRepo  ports.ScenarioIterationRepository
	ruleRepo  ports.RuleRepository
	validator IterationValidator
}

func NewIterationService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	readRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
	validator IterationValidator,
) IterationService {
	return IterationService{
		txManager: txManager,
		idGen:     idGen,
		clock:     clock,
		readRepo:  readRepo,
		ruleRepo:  ruleRepo,
		validator: validator,
	}
}

func (s IterationService) CreateDraft(ctx context.Context, tenantID, scenarioID string) (scenario.Iteration, error) {
	var created scenario.Iteration
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		version, err := store.Iterations().NextVersion(ctx, tenantID, scenarioID)
		if err != nil {
			return err
		}
		item := scenario.Iteration{
			ID:                           s.idGen.New().String(),
			ScenarioID:                   scenarioID,
			TenantID:                     tenantID,
			Version:                      version,
			Status:                       scenario.IterationStatusDraft,
			TriggerFormula:               json.RawMessage(`{"constant":true}`),
			ScoreReviewThreshold:         intPtr(1),
			ScoreBlockAndReviewThreshold: intPtr(10),
			ScoreDeclineThreshold:        intPtr(20),
			CreatedAt:                    s.clock.Now(),
		}
		if err := item.Validate(); err != nil {
			return err
		}
		created, err = store.Iterations().Create(ctx, item)
		return err
	})
	return created, err
}

func (s IterationService) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.Iteration, error) {
	return s.readRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s IterationService) GetByID(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.Iteration, error) {
	return s.readRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
}

func (s IterationService) CreateDraftFromIteration(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.Iteration, error) {
	var created scenario.Iteration
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		source, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		version, err := store.Iterations().NextVersion(ctx, tenantID, scenarioID)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		cloned := source
		cloned.ID = s.idGen.New().String()
		cloned.Version = version
		cloned.Status = scenario.IterationStatusDraft
		cloned.CreatedAt = now
		cloned.CommittedAt = nil
		if err := cloned.Validate(); err != nil {
			return err
		}
		created, err = store.Iterations().Create(ctx, cloned)
		if err != nil {
			return err
		}

		rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			clonedRule := rule
			clonedRule.ID = s.idGen.New().String()
			clonedRule.IterationID = created.ID
			clonedRule.CreatedAt = now
			clonedRule.UpdatedAt = now
			if _, err := store.Rules().Create(ctx, clonedRule); err != nil {
				return err
			}
		}
		return nil
	})
	return created, err
}

func (s IterationService) UpdateDraft(
	ctx context.Context,
	tenantID, scenarioID, iterationID string,
	triggerFormula json.RawMessage,
	scoreReviewThreshold, scoreBlockAndReviewThreshold, scoreDeclineThreshold *int,
	schedule string,
) (scenario.Iteration, error) {
	var updated scenario.Iteration
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		current, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if current.Status != scenario.IterationStatusDraft {
			return scenarioError("iteration is not draft")
		}
		current.TriggerFormula = triggerFormula
		current.ScoreReviewThreshold = scoreReviewThreshold
		current.ScoreBlockAndReviewThreshold = scoreBlockAndReviewThreshold
		current.ScoreDeclineThreshold = scoreDeclineThreshold
		current.Schedule = schedule
		if err := current.Validate(); err != nil {
			return err
		}
		updated, err = store.Iterations().Update(ctx, current)
		return err
	})
	return updated, err
}

func (s IterationService) Commit(ctx context.Context, tenantID, scenarioID, iterationID string) (scenario.Iteration, error) {
	if s.validator != nil {
		validation, err := s.validator.ValidateIteration(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return scenario.Iteration{}, err
		}
		if !validation.Valid {
			return scenario.Iteration{}, scenarioError("iteration validation failed")
		}
	}

	var committed scenario.Iteration
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		current, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if current.Status != scenario.IterationStatusDraft {
			return scenarioError("iteration is not draft")
		}
		committed, err = store.Iterations().Commit(ctx, tenantID, scenarioID, iterationID, s.clock.Now())
		return err
	})
	return committed, err
}

type scenarioError string

func (e scenarioError) Error() string { return string(e) }

func intPtr(v int) *int { return &v }
