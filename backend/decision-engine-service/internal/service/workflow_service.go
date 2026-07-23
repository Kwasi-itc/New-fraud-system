package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type WorkflowService struct {
	txManager        ports.TransactionManager
	idGen            ports.IDGenerator
	clock            ports.Clock
	scenarioRepo     ports.ScenarioRepository
	workflowRepo     ports.WorkflowRepository
	executionRepo    ports.WorkflowExecutionRepository
	cacheInvalidator DecisionMetadataCacheInvalidator
}

func NewWorkflowService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	scenarioRepo ports.ScenarioRepository,
	workflowRepo ports.WorkflowRepository,
	executionRepo ports.WorkflowExecutionRepository,
) WorkflowService {
	return WorkflowService{
		txManager:     txManager,
		idGen:         idGen,
		clock:         clock,
		scenarioRepo:  scenarioRepo,
		workflowRepo:  workflowRepo,
		executionRepo: executionRepo,
	}
}

func (s *WorkflowService) SetCacheInvalidator(invalidator DecisionMetadataCacheInvalidator) {
	s.cacheInvalidator = invalidator
}

func (s WorkflowService) Create(
	ctx context.Context,
	tenantID, scenarioID, name, description string,
	allowedOutcomes []string,
	actionType string,
	actionConfig json.RawMessage,
	active bool,
) (workflow.Definition, error) {
	if _, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID); err != nil {
		return workflow.Definition{}, err
	}
	existing, err := s.workflowRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return workflow.Definition{}, err
	}
	displayOrder := 0
	for _, current := range existing {
		if current.DisplayOrder >= displayOrder {
			displayOrder = current.DisplayOrder + 1
		}
	}
	now := s.clock.Now()
	item := workflow.Definition{
		ID:              s.idGen.New().String(),
		TenantID:        tenantID,
		ScenarioID:      scenarioID,
		DisplayOrder:    displayOrder,
		Name:            name,
		Description:     description,
		AllowedOutcomes: allowedOutcomes,
		ActionType:      workflow.ActionType(actionType),
		ActionConfig:    actionConfig,
		Active:          active,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := item.Validate(); err != nil {
		return workflow.Definition{}, err
	}

	var created workflow.Definition
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.Workflows().Create(ctx, item)
		return err
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateActiveWorkflows(ctx, tenantID, scenarioID)
	}
	return created, err
}

func (s WorkflowService) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error) {
	return s.workflowRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s WorkflowService) GetByID(ctx context.Context, tenantID, scenarioID, workflowID string) (workflow.Definition, error) {
	return s.workflowRepo.GetByID(ctx, tenantID, scenarioID, workflowID)
}

func (s WorkflowService) Update(
	ctx context.Context,
	tenantID, scenarioID, workflowID, name, description string,
	allowedOutcomes []string,
	actionType string,
	actionConfig json.RawMessage,
	active bool,
) (workflow.Definition, error) {
	current, err := s.workflowRepo.GetByID(ctx, tenantID, scenarioID, workflowID)
	if err != nil {
		return workflow.Definition{}, err
	}
	current.Name = name
	current.Description = description
	current.AllowedOutcomes = allowedOutcomes
	current.ActionType = workflow.ActionType(actionType)
	current.ActionConfig = actionConfig
	current.Active = active
	current.UpdatedAt = s.clock.Now()
	if err := current.Validate(); err != nil {
		return workflow.Definition{}, err
	}

	var updated workflow.Definition
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.Workflows().Update(ctx, current)
		return runErr
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateActiveWorkflows(ctx, tenantID, scenarioID)
	}
	return updated, err
}

func (s WorkflowService) Delete(ctx context.Context, tenantID, scenarioID, workflowID string) error {
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.Workflows().Delete(ctx, tenantID, scenarioID, workflowID)
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateActiveWorkflows(ctx, tenantID, scenarioID)
	}
	return err
}

func (s WorkflowService) Reorder(ctx context.Context, tenantID, scenarioID string, orderedIDs []string) error {
	items, err := s.workflowRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return err
	}
	if len(items) != len(orderedIDs) {
		return fmt.Errorf("workflow_ids must include every workflow exactly once")
	}

	expected := make(map[string]struct{}, len(items))
	for _, item := range items {
		expected[item.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(orderedIDs))
	for _, id := range orderedIDs {
		if _, ok := expected[id]; !ok {
			return fmt.Errorf("workflow %q does not belong to scenario", id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("workflow %q is duplicated", id)
		}
		seen[id] = struct{}{}
	}

	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.Workflows().Reorder(ctx, tenantID, scenarioID, orderedIDs, s.clock.Now())
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateActiveWorkflows(ctx, tenantID, scenarioID)
	}
	return err
}

func (s WorkflowService) ListExecutionsByDecision(ctx context.Context, tenantID, decisionID string) ([]workflow.Execution, error) {
	return s.executionRepo.ListByDecision(ctx, tenantID, decisionID)
}
