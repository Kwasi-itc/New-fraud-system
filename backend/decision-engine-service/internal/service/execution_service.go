package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type AsyncDecisionExecutionRequest struct {
	ScenarioID string                      `json:"scenario_id"`
	ObjectType string                      `json:"object_type"`
	Items      []DecisionEvaluationRequest `json:"items"`
}

type ScheduledExecutionRequest struct {
	Items          []DecisionEvaluationRequest `json:"items"`
	CandidateLimit int                         `json:"candidate_limit"`
}

type ExecutionService struct {
	txManager        ports.TransactionManager
	idGen            ports.IDGenerator
	clock            ports.Clock
	scenarioRepo     ports.ScenarioRepository
	tenantDataReader ports.TenantDataReader
	scheduledRepo    ports.ScheduledExecutionRepository
	asyncRepo        ports.AsyncDecisionExecutionRepository
	decisionSvc      DecisionService
}

func NewExecutionService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	scenarioRepo ports.ScenarioRepository,
	tenantDataReader ports.TenantDataReader,
	scheduledRepo ports.ScheduledExecutionRepository,
	asyncRepo ports.AsyncDecisionExecutionRepository,
	decisionSvc DecisionService,
) ExecutionService {
	return ExecutionService{
		txManager:        txManager,
		idGen:            idGen,
		clock:            clock,
		scenarioRepo:     scenarioRepo,
		tenantDataReader: tenantDataReader,
		scheduledRepo:    scheduledRepo,
		asyncRepo:        asyncRepo,
		decisionSvc:      decisionSvc,
	}
}

func (s ExecutionService) CreateScheduledExecution(ctx context.Context, tenantID, scenarioID string, scheduledFor time.Time, req ScheduledExecutionRequest) (execution.ScheduledExecution, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return execution.ScheduledExecution{}, err
	}
	if scn.LiveIterationID == nil {
		return execution.ScheduledExecution{}, fmt.Errorf("scenario has no live iteration")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return execution.ScheduledExecution{}, err
	}
	now := s.clock.Now()
	item := execution.ScheduledExecution{
		ID:                  s.idGen.New().String(),
		TenantID:            tenantID,
		ScenarioID:          scenarioID,
		ScenarioIterationID: *scn.LiveIterationID,
		Status:              execution.StatusPending,
		ScheduledFor:        scheduledFor,
		RequestBody:         body,
		CreatedAt:           now,
	}
	var created execution.ScheduledExecution
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.ScheduledExecutions().Create(ctx, item)
		return err
	})
	return created, err
}

func (s ExecutionService) ListScheduledExecutionsByScenario(ctx context.Context, tenantID, scenarioID string) ([]execution.ScheduledExecution, error) {
	return s.scheduledRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s ExecutionService) CreateAsyncDecisionExecution(ctx context.Context, tenantID string, req AsyncDecisionExecutionRequest) (execution.AsyncDecisionExecution, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return execution.AsyncDecisionExecution{}, err
	}
	now := s.clock.Now()
	item := execution.AsyncDecisionExecution{
		ID:          s.idGen.New().String(),
		TenantID:    tenantID,
		ScenarioID:  req.ScenarioID,
		ObjectType:  req.ObjectType,
		Status:      execution.StatusQueued,
		RequestBody: body,
		CreatedAt:   now,
	}
	var created execution.AsyncDecisionExecution
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.AsyncDecisionExecutions().Create(ctx, item)
		return err
	})
	return created, err
}

func (s ExecutionService) ListAsyncDecisionExecutionsByTenant(ctx context.Context, tenantID string) ([]execution.AsyncDecisionExecution, error) {
	return s.asyncRepo.ListByTenant(ctx, tenantID)
}

func (s ExecutionService) ProcessDueScheduledExecutions(ctx context.Context, limit int) error {
	items, err := s.scheduledRepo.ListDue(ctx, s.clock.Now(), limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.scheduledRepo.UpdateStatus(ctx, item.ID, execution.StatusRunning); err != nil {
			return err
		}
		if err := s.runScheduledExecution(ctx, item); err != nil {
			_ = s.scheduledRepo.UpdateStatus(ctx, item.ID, execution.StatusFailed)
			return err
		}
		if err := s.scheduledRepo.UpdateStatus(ctx, item.ID, execution.StatusCompleted); err != nil {
			return err
		}
	}
	return nil
}

func (s ExecutionService) ProcessQueuedAsyncExecutions(ctx context.Context, limit int) error {
	items, err := s.asyncRepo.ListQueued(ctx, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.asyncRepo.UpdateStatus(ctx, item.ID, execution.StatusRunning); err != nil {
			return err
		}
		if err := s.runAsyncExecution(ctx, item); err != nil {
			_ = s.asyncRepo.UpdateStatus(ctx, item.ID, execution.StatusFailed)
			return err
		}
		if err := s.asyncRepo.UpdateStatus(ctx, item.ID, execution.StatusCompleted); err != nil {
			return err
		}
	}
	return nil
}

func (s ExecutionService) runScheduledExecution(ctx context.Context, item execution.ScheduledExecution) error {
	var req ScheduledExecutionRequest
	if err := json.Unmarshal(item.RequestBody, &req); err != nil {
		return err
	}
	if len(req.Items) == 0 {
		scn, err := s.scenarioRepo.GetByID(ctx, item.TenantID, item.ScenarioID)
		if err != nil {
			return err
		}
		limit := req.CandidateLimit
		if limit <= 0 {
			limit = 100
		}
		records, err := s.tenantDataReader.ListRecords(ctx, item.TenantID, scn.TriggerObjectType, limit)
		if err != nil {
			return err
		}
		req.Items = make([]DecisionEvaluationRequest, len(records))
		for i, record := range records {
			req.Items[i] = DecisionEvaluationRequest{
				ObjectID:   record.ObjectID,
				ObjectType: record.ObjectType,
				Fields:     record.Fields,
			}
		}
	}
	for _, evalReq := range req.Items {
		if _, err := s.decisionSvc.EvaluateScenario(ctx, item.TenantID, item.ScenarioID, evalReq); err != nil {
			return err
		}
	}
	return nil
}

func (s ExecutionService) runAsyncExecution(ctx context.Context, item execution.AsyncDecisionExecution) error {
	var req AsyncDecisionExecutionRequest
	if err := json.Unmarshal(item.RequestBody, &req); err != nil {
		return err
	}
	for _, evalReq := range req.Items {
		if req.ScenarioID != "" {
			if _, err := s.decisionSvc.EvaluateScenario(ctx, item.TenantID, req.ScenarioID, evalReq); err != nil {
				return err
			}
			continue
		}
		if _, err := s.decisionSvc.EvaluateAllLiveScenarios(ctx, item.TenantID, evalReq); err != nil {
			return err
		}
	}
	return nil
}
