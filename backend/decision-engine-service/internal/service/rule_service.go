package service

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type RuleService struct {
	txManager ports.TransactionManager
	idGen     ports.IDGenerator
	clock     ports.Clock
	ruleRepo  ports.RuleRepository
	iterRepo  ports.ScenarioIterationRepository
}

func NewRuleService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	ruleRepo ports.RuleRepository,
	iterRepo ports.ScenarioIterationRepository,
) RuleService {
	return RuleService{
		txManager: txManager,
		idGen:     idGen,
		clock:     clock,
		ruleRepo:  ruleRepo,
		iterRepo:  iterRepo,
	}
}

func (s RuleService) ListByIteration(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenario.Rule, error) {
	return s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
}

func (s RuleService) ListRuleGroupsByScenario(ctx context.Context, tenantID, scenarioID string) ([]string, error) {
	return s.ruleRepo.ListRuleGroupsByScenario(ctx, tenantID, scenarioID)
}

func (s RuleService) Create(
	ctx context.Context,
	tenantID, scenarioID, iterationID string,
	displayOrder int,
	name, description string,
	formula json.RawMessage,
	scoreModifier int,
	ruleGroup string,
	snoozeGroupID *string,
	stableRuleID string,
) (scenario.Rule, error) {
	var created scenario.Rule
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		iteration, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if iteration.Status != scenario.IterationStatusDraft {
			return scenarioError("rules can only be created on draft iterations")
		}

		now := s.clock.Now()
		item := scenario.Rule{
			ID:            s.idGen.New().String(),
			IterationID:   iterationID,
			TenantID:      tenantID,
			DisplayOrder:  displayOrder,
			Name:          name,
			Description:   description,
			Formula:       formula,
			ScoreModifier: scoreModifier,
			RuleGroup:     ruleGroup,
			SnoozeGroupID: snoozeGroupID,
			StableRuleID:  stableRuleID,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		created, err = store.Rules().Create(ctx, item)
		return err
	})
	return created, err
}

func (s RuleService) Update(
	ctx context.Context,
	tenantID, scenarioID, iterationID, ruleID string,
	displayOrder int,
	name, description string,
	formula json.RawMessage,
	scoreModifier int,
	ruleGroup string,
	snoozeGroupID *string,
	stableRuleID string,
) (scenario.Rule, error) {
	var updated scenario.Rule
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		iteration, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if iteration.Status != scenario.IterationStatusDraft {
			return scenarioError("rules can only be updated on draft iterations")
		}

		existing, err := store.Rules().GetByID(ctx, tenantID, scenarioID, iterationID, ruleID)
		if err != nil {
			return err
		}
		existing.DisplayOrder = displayOrder
		existing.Name = name
		existing.Description = description
		existing.Formula = formula
		existing.ScoreModifier = scoreModifier
		existing.RuleGroup = ruleGroup
		existing.SnoozeGroupID = snoozeGroupID
		existing.StableRuleID = stableRuleID
		existing.UpdatedAt = s.clock.Now()
		if err := existing.Validate(); err != nil {
			return err
		}
		updated, err = store.Rules().Update(ctx, existing)
		return err
	})
	return updated, err
}

func (s RuleService) Delete(ctx context.Context, tenantID, scenarioID, iterationID, ruleID string) error {
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		iteration, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, iterationID)
		if err != nil {
			return err
		}
		if iteration.Status != scenario.IterationStatusDraft {
			return scenarioError("rules can only be deleted on draft iterations")
		}
		return store.Rules().Delete(ctx, tenantID, scenarioID, iterationID, ruleID)
	})
}
