package service

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type ScenarioService struct {
	txManager          ports.TransactionManager
	idGen              ports.IDGenerator
	clock              ports.Clock
	readRepo           ports.ScenarioRepository
	iterRepo           ports.ScenarioIterationRepository
	ruleRepo           ports.RuleRepository
	workflowRuleRepo   ports.WorkflowRuleRepository
	workflowCondRepo   ports.WorkflowConditionRepository
	workflowActionRepo ports.WorkflowActionRepository
}

func NewScenarioService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	readRepo ports.ScenarioRepository,
	iterRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
	workflowRuleRepo ports.WorkflowRuleRepository,
	workflowCondRepo ports.WorkflowConditionRepository,
	workflowActionRepo ports.WorkflowActionRepository,
) ScenarioService {
	return ScenarioService{
		txManager:          txManager,
		idGen:              idGen,
		clock:              clock,
		readRepo:           readRepo,
		iterRepo:           iterRepo,
		ruleRepo:           ruleRepo,
		workflowRuleRepo:   workflowRuleRepo,
		workflowCondRepo:   workflowCondRepo,
		workflowActionRepo: workflowActionRepo,
	}
}

func (s ScenarioService) Create(ctx context.Context, tenantID, name, triggerObjectType string) (scenario.Scenario, error) {
	now := s.clock.Now()
	item := scenario.Scenario{
		ID:                s.idGen.New().String(),
		TenantID:          tenantID,
		Name:              name,
		TriggerObjectType: triggerObjectType,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := item.Validate(); err != nil {
		return scenario.Scenario{}, err
	}

	var created scenario.Scenario
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.Scenarios().Create(ctx, item)
		return err
	})
	return created, err
}

func (s ScenarioService) ListByTenant(ctx context.Context, tenantID string) ([]scenario.Scenario, error) {
	return s.readRepo.ListByTenant(ctx, tenantID)
}

func (s ScenarioService) GetByID(ctx context.Context, tenantID, scenarioID string) (scenario.Scenario, error) {
	return s.readRepo.GetByID(ctx, tenantID, scenarioID)
}

func (s ScenarioService) Update(ctx context.Context, tenantID, scenarioID, name, triggerObjectType string) (scenario.Scenario, error) {
	current, err := s.readRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return scenario.Scenario{}, err
	}
	current.Name = name
	current.TriggerObjectType = triggerObjectType
	current.UpdatedAt = s.clock.Now()
	if err := current.Validate(); err != nil {
		return scenario.Scenario{}, err
	}

	var updated scenario.Scenario
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.Scenarios().Update(ctx, current)
		return runErr
	})
	return updated, err
}

func (s ScenarioService) Copy(ctx context.Context, tenantID, scenarioID, name string) (scenario.Scenario, error) {
	source, err := s.readRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return scenario.Scenario{}, err
	}

	iterations, err := s.iterRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return scenario.Scenario{}, err
	}
	workflowRules, err := s.workflowRuleRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return scenario.Scenario{}, err
	}

	now := s.clock.Now()
	copiedScenario := scenario.Scenario{
		ID:                s.idGen.New().String(),
		TenantID:          tenantID,
		Name:              name,
		TriggerObjectType: source.TriggerObjectType,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if copiedScenario.Name == "" {
		copiedScenario.Name = source.Name + " Copy"
	}
	if err := copiedScenario.Validate(); err != nil {
		return scenario.Scenario{}, err
	}

	var created scenario.Scenario
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.Scenarios().Create(ctx, copiedScenario)
		if runErr != nil {
			return runErr
		}

		var liveIterationID *string
		for _, item := range iterations {
			newIterationID := s.idGen.New().String()
			cloned := item
			cloned.ID = newIterationID
			cloned.ScenarioID = created.ID
			cloned.TenantID = tenantID
			cloned.CreatedAt = now
			if cloned.Status == scenario.IterationStatusCommitted {
				committedAt := now
				cloned.CommittedAt = &committedAt
			}
			createdIteration, createErr := store.Iterations().Create(ctx, cloned)
			if createErr != nil {
				return createErr
			}
			if source.LiveIterationID != nil && item.ID == *source.LiveIterationID {
				liveIterationID = &createdIteration.ID
			}

			rules, listErr := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, item.ID)
			if listErr != nil {
				return listErr
			}
			for _, rule := range rules {
				clonedRule := rule
				clonedRule.ID = s.idGen.New().String()
				clonedRule.IterationID = createdIteration.ID
				clonedRule.TenantID = tenantID
				clonedRule.StableRuleID = s.idGen.New().String()
				clonedRule.CreatedAt = now
				clonedRule.UpdatedAt = now
				if _, createErr := store.Rules().Create(ctx, clonedRule); createErr != nil {
					return createErr
				}
			}
		}

		for _, workflowRule := range workflowRules {
			clonedRule := workflowRule
			clonedRule.ID = s.idGen.New().String()
			clonedRule.ScenarioID = created.ID
			clonedRule.TenantID = tenantID
			clonedRule.CreatedAt = now
			clonedRule.UpdatedAt = now
			createdWorkflowRule, createErr := store.WorkflowRules().Create(ctx, clonedRule)
			if createErr != nil {
				return createErr
			}
			conditions, listErr := s.workflowCondRepo.ListByRule(ctx, tenantID, workflowRule.ID)
			if listErr != nil {
				return listErr
			}
			for _, condition := range conditions {
				clonedCondition := condition
				clonedCondition.ID = s.idGen.New().String()
				clonedCondition.TenantID = tenantID
				clonedCondition.RuleID = createdWorkflowRule.ID
				clonedCondition.CreatedAt = now
				clonedCondition.UpdatedAt = now
				if _, createErr := store.WorkflowConditions().Create(ctx, clonedCondition); createErr != nil {
					return createErr
				}
			}

			actions, listErr := s.workflowActionRepo.ListByRule(ctx, tenantID, workflowRule.ID)
			if listErr != nil {
				return listErr
			}
			for _, action := range actions {
				clonedAction := action
				clonedAction.ID = s.idGen.New().String()
				clonedAction.TenantID = tenantID
				clonedAction.RuleID = createdWorkflowRule.ID
				clonedAction.CreatedAt = now
				clonedAction.UpdatedAt = now
				if _, createErr := store.WorkflowActions().Create(ctx, clonedAction); createErr != nil {
					return createErr
				}
			}
		}

		if liveIterationID != nil {
			created.LiveIterationID = liveIterationID
			created.UpdatedAt = now
			created, runErr = store.Scenarios().Update(ctx, created)
			if runErr != nil {
				return runErr
			}
		}
		return nil
	})
	return created, err
}

func (s ScenarioService) ListLatestRules(ctx context.Context, tenantID, scenarioID string) ([]scenario.Rule, error) {
	scn, err := s.readRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}

	iterations, err := s.iterRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	if len(iterations) == 0 {
		return []scenario.Rule{}, nil
	}

	target := iterations[0]
	if scn.LiveIterationID != nil {
		for _, item := range iterations {
			if item.ID == *scn.LiveIterationID {
				target = item
				break
			}
		}
	}

	return s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, target.ID)
}
