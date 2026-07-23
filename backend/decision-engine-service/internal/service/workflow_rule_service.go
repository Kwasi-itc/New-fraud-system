package service

import (
	"context"
	"encoding/json"
	"fmt"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type WorkflowRuleService struct {
	txManager        ports.TransactionManager
	idGen            ports.IDGenerator
	clock            ports.Clock
	dataModelReader  ports.DataModelReader
	scenarioRepo     ports.ScenarioRepository
	workflowRuleRepo ports.WorkflowRuleRepository
	conditionRepo    ports.WorkflowConditionRepository
	actionRepo       ports.WorkflowActionRepository
	cacheInvalidator DecisionMetadataCacheInvalidator
}

func NewWorkflowRuleService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	dataModelReader ports.DataModelReader,
	scenarioRepo ports.ScenarioRepository,
	workflowRuleRepo ports.WorkflowRuleRepository,
	conditionRepo ports.WorkflowConditionRepository,
	actionRepo ports.WorkflowActionRepository,
) WorkflowRuleService {
	return WorkflowRuleService{
		txManager:        txManager,
		idGen:            idGen,
		clock:            clock,
		dataModelReader:  dataModelReader,
		scenarioRepo:     scenarioRepo,
		workflowRuleRepo: workflowRuleRepo,
		conditionRepo:    conditionRepo,
		actionRepo:       actionRepo,
	}
}

func (s *WorkflowRuleService) SetCacheInvalidator(invalidator DecisionMetadataCacheInvalidator) {
	s.cacheInvalidator = invalidator
}

func (s WorkflowRuleService) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.StructuredRule, error) {
	rules, err := s.workflowRuleRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	return s.aggregate(ctx, tenantID, rules)
}

func (s WorkflowRuleService) GetByID(ctx context.Context, tenantID, scenarioID, ruleID string) (workflow.StructuredRule, error) {
	rule, err := s.workflowRuleRepo.GetByID(ctx, tenantID, scenarioID, ruleID)
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	items, err := s.aggregate(ctx, tenantID, []workflow.Rule{rule})
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	return items[0], nil
}

func (s WorkflowRuleService) CreateRule(ctx context.Context, tenantID, scenarioID, name string, fallthroughEnabled bool) (workflow.StructuredRule, error) {
	if _, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID); err != nil {
		return workflow.StructuredRule{}, err
	}
	existing, err := s.workflowRuleRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	priority := 0
	for _, item := range existing {
		if item.Priority >= priority {
			priority = item.Priority + 1
		}
	}
	now := s.clock.Now()
	item := workflow.Rule{
		ID:          s.idGen.New().String(),
		TenantID:    tenantID,
		ScenarioID:  scenarioID,
		Name:        name,
		Priority:    priority,
		Fallthrough: fallthroughEnabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := item.Validate(); err != nil {
		return workflow.StructuredRule{}, err
	}
	var created workflow.Rule
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.WorkflowRules().Create(ctx, item)
		return runErr
	})
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	if s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return workflow.StructuredRule{Rule: created}, nil
}

func (s WorkflowRuleService) UpdateRule(ctx context.Context, tenantID, scenarioID, ruleID, name string, fallthroughEnabled bool) (workflow.StructuredRule, error) {
	current, err := s.workflowRuleRepo.GetByID(ctx, tenantID, scenarioID, ruleID)
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	current.Name = name
	current.Fallthrough = fallthroughEnabled
	current.UpdatedAt = s.clock.Now()
	if err := current.Validate(); err != nil {
		return workflow.StructuredRule{}, err
	}
	var updated workflow.Rule
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.WorkflowRules().Update(ctx, current)
		return runErr
	})
	if err != nil {
		return workflow.StructuredRule{}, err
	}
	if s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return s.GetByID(ctx, tenantID, scenarioID, updated.ID)
}

func (s WorkflowRuleService) DeleteRule(ctx context.Context, tenantID, scenarioID, ruleID string) error {
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.WorkflowRules().Delete(ctx, tenantID, scenarioID, ruleID)
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return err
}

func (s WorkflowRuleService) ReorderRules(ctx context.Context, tenantID, scenarioID string, orderedIDs []string) error {
	items, err := s.workflowRuleRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return err
	}
	if len(items) != len(orderedIDs) {
		return fmt.Errorf("workflow_rule_ids must include every workflow rule exactly once")
	}
	expected := make(map[string]struct{}, len(items))
	for _, item := range items {
		expected[item.ID] = struct{}{}
	}
	for _, id := range orderedIDs {
		if _, ok := expected[id]; !ok {
			return fmt.Errorf("workflow rule %q does not belong to scenario", id)
		}
		delete(expected, id)
	}
	if len(expected) != 0 {
		return fmt.Errorf("workflow_rule_ids must include every workflow rule exactly once")
	}
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.WorkflowRules().Reorder(ctx, tenantID, scenarioID, orderedIDs, s.clock.Now())
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return err
}

func (s WorkflowRuleService) CreateCondition(ctx context.Context, tenantID, scenarioID, ruleID, function string, params json.RawMessage) (workflow.Condition, error) {
	if _, err := s.workflowRuleRepo.GetByID(ctx, tenantID, scenarioID, ruleID); err != nil {
		return workflow.Condition{}, err
	}
	item := workflow.Condition{
		ID:        s.idGen.New().String(),
		TenantID:  tenantID,
		RuleID:    ruleID,
		Function:  workflow.ConditionType(function),
		Params:    params,
		CreatedAt: s.clock.Now(),
		UpdatedAt: s.clock.Now(),
	}
	if err := s.validateCondition(ctx, tenantID, scenarioID, item); err != nil {
		return workflow.Condition{}, err
	}
	var created workflow.Condition
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.WorkflowConditions().Create(ctx, item)
		return runErr
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return created, err
}

func (s WorkflowRuleService) UpdateCondition(ctx context.Context, tenantID, scenarioID, ruleID, conditionID, function string, params json.RawMessage) (workflow.Condition, error) {
	item, err := s.conditionRepo.GetByID(ctx, tenantID, ruleID, conditionID)
	if err != nil {
		return workflow.Condition{}, err
	}
	item.Function = workflow.ConditionType(function)
	item.Params = params
	item.UpdatedAt = s.clock.Now()
	if err := s.validateCondition(ctx, tenantID, scenarioID, item); err != nil {
		return workflow.Condition{}, err
	}
	var updated workflow.Condition
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.WorkflowConditions().Update(ctx, item)
		return runErr
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return updated, err
}

func (s WorkflowRuleService) DeleteCondition(ctx context.Context, tenantID, ruleID, conditionID string) error {
	scenarioID, err := s.scenarioIDForWorkflowRule(ctx, tenantID, ruleID)
	if err != nil {
		return err
	}
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.WorkflowConditions().Delete(ctx, tenantID, ruleID, conditionID)
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return err
}

func (s WorkflowRuleService) CreateAction(ctx context.Context, tenantID, scenarioID, ruleID, actionType string, actionConfig json.RawMessage) (workflow.Action, error) {
	if _, err := s.workflowRuleRepo.GetByID(ctx, tenantID, scenarioID, ruleID); err != nil {
		return workflow.Action{}, err
	}
	item := workflow.Action{
		ID:           s.idGen.New().String(),
		TenantID:     tenantID,
		RuleID:       ruleID,
		ActionType:   workflow.ActionType(actionType),
		ActionConfig: actionConfig,
		CreatedAt:    s.clock.Now(),
		UpdatedAt:    s.clock.Now(),
	}
	if err := item.Validate(); err != nil {
		return workflow.Action{}, err
	}
	var created workflow.Action
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		created, runErr = store.WorkflowActions().Create(ctx, item)
		return runErr
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return created, err
}

func (s WorkflowRuleService) UpdateAction(ctx context.Context, tenantID, ruleID, actionID, actionType string, actionConfig json.RawMessage) (workflow.Action, error) {
	scenarioID, err := s.scenarioIDForWorkflowRule(ctx, tenantID, ruleID)
	if err != nil {
		return workflow.Action{}, err
	}
	item, err := s.actionRepo.GetByID(ctx, tenantID, ruleID, actionID)
	if err != nil {
		return workflow.Action{}, err
	}
	item.ActionType = workflow.ActionType(actionType)
	item.ActionConfig = actionConfig
	item.UpdatedAt = s.clock.Now()
	if err := item.Validate(); err != nil {
		return workflow.Action{}, err
	}
	var updated workflow.Action
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.WorkflowActions().Update(ctx, item)
		return runErr
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return updated, err
}

func (s WorkflowRuleService) DeleteAction(ctx context.Context, tenantID, ruleID, actionID string) error {
	scenarioID, err := s.scenarioIDForWorkflowRule(ctx, tenantID, ruleID)
	if err != nil {
		return err
	}
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.WorkflowActions().Delete(ctx, tenantID, ruleID, actionID)
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateWorkflowRules(ctx, tenantID, scenarioID)
	}
	return err
}

func (s WorkflowRuleService) scenarioIDForWorkflowRule(ctx context.Context, tenantID, ruleID string) (string, error) {
	scenarios, err := s.scenarioRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return "", err
	}
	for _, scenarioItem := range scenarios {
		rules, listErr := s.workflowRuleRepo.ListByScenario(ctx, tenantID, scenarioItem.ID)
		if listErr != nil {
			return "", listErr
		}
		for _, rule := range rules {
			if rule.ID == ruleID {
				return scenarioItem.ID, nil
			}
		}
	}
	return "", fmt.Errorf("workflow rule %q not found", ruleID)
}

func (s WorkflowRuleService) aggregate(ctx context.Context, tenantID string, rules []workflow.Rule) ([]workflow.StructuredRule, error) {
	out := make([]workflow.StructuredRule, 0, len(rules))
	for _, rule := range rules {
		conditions, err := s.conditionRepo.ListByRule(ctx, tenantID, rule.ID)
		if err != nil {
			return nil, err
		}
		actions, err := s.actionRepo.ListByRule(ctx, tenantID, rule.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, workflow.StructuredRule{Rule: rule, Conditions: conditions, Actions: actions})
	}
	return out, nil
}

func (s WorkflowRuleService) validateCondition(ctx context.Context, tenantID, scenarioID string, item workflow.Condition) error {
	if err := item.Validate(); err != nil {
		return err
	}
	switch item.Function {
	case workflow.ConditionAlways, workflow.ConditionNever:
		if len(item.Params) > 0 && string(item.Params) != "null" {
			return fmt.Errorf("condition %s does not take params", item.Function)
		}
	case workflow.ConditionOutcomeIn:
		var outcomes []string
		if err := json.Unmarshal(item.Params, &outcomes); err != nil {
			return err
		}
		if len(outcomes) == 0 {
			return fmt.Errorf("at least one outcome is required")
		}
		for _, outcome := range outcomes {
			switch decision.Outcome(outcome) {
			case decision.OutcomeApprove, decision.OutcomeReview, decision.OutcomeBlockAndReview, decision.OutcomeDecline:
			default:
				return fmt.Errorf("invalid outcome %q", outcome)
			}
		}
	case workflow.ConditionRuleHit:
		var params ruleHitParams
		if err := json.Unmarshal(item.Params, &params); err != nil {
			return err
		}
		if len(params.IDs()) == 0 {
			return fmt.Errorf("at least one rule id is required")
		}
	case workflow.ConditionPayloadEvaluates:
		var params evaluatesParams
		if err := json.Unmarshal(item.Params, &params); err != nil {
			return err
		}
		if len(params.Expression) == 0 {
			return fmt.Errorf("expression is required")
		}
		scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
		if err != nil {
			return err
		}
		model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
		if err != nil {
			return err
		}
		var node domainast.Node
		if err := json.Unmarshal(params.Expression, &node); err != nil {
			return err
		}
		valueType, errs := asteval.ValidateNode(node, model, scn.TriggerObjectType)
		if len(errs) > 0 {
			return fmt.Errorf("invalid payload expression: %v", errs)
		}
		if valueType != domainast.ValueTypeBool {
			return fmt.Errorf("payload expression must return a boolean")
		}
	default:
		return fmt.Errorf("unknown condition function %q", item.Function)
	}
	return nil
}

type ruleHitParams struct {
	RuleID  []string `json:"rule_id"`
	RuleIDs []string `json:"rule_ids"`
}

func (p ruleHitParams) IDs() []string {
	if len(p.RuleIDs) != 0 {
		return p.RuleIDs
	}
	return p.RuleID
}

type evaluatesParams struct {
	Expression json.RawMessage `json:"expression"`
}
