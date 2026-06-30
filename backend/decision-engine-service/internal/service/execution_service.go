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
	iterationRepo    ports.ScenarioIterationRepository
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
	iterationRepo ports.ScenarioIterationRepository,
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
		iterationRepo:    iterationRepo,
		tenantDataReader: tenantDataReader,
		scheduledRepo:    scheduledRepo,
		asyncRepo:        asyncRepo,
		decisionSvc:      decisionSvc,
	}
}

func (s ExecutionService) GetRecurringSchedule(ctx context.Context, tenantID, scenarioID string) (RecurringScheduleConfig, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	if scn.LiveIterationID == nil {
		return RecurringScheduleConfig{}, fmt.Errorf("scenario has no live iteration")
	}

	iteration, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, *scn.LiveIterationID)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	return DecodeRecurringScheduleConfig(iteration.Schedule)
}

func (s ExecutionService) UpdateRecurringSchedule(ctx context.Context, tenantID, scenarioID string, cfg RecurringScheduleConfig) (RecurringScheduleConfig, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	if scn.LiveIterationID == nil {
		return RecurringScheduleConfig{}, fmt.Errorf("scenario has no live iteration")
	}

	normalized, err := NormalizeRecurringScheduleConfig(cfg)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	encoded, err := EncodeRecurringScheduleConfig(normalized)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}

	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		iteration, err := store.Iterations().GetByID(ctx, tenantID, scenarioID, *scn.LiveIterationID)
		if err != nil {
			return err
		}
		iteration.Schedule = encoded
		_, err = store.Iterations().Update(ctx, iteration)
		return err
	})
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	return normalized, nil
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

func (s ExecutionService) GetScheduledExecutionByID(ctx context.Context, tenantID, scenarioID, executionID string) (execution.ScheduledExecution, error) {
	return s.scheduledRepo.GetByID(ctx, tenantID, scenarioID, executionID)
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
	if err := s.materializeRecurringSchedules(ctx, limit); err != nil {
		return err
	}

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
			continue
		}
		if err := s.scheduledRepo.UpdateStatus(ctx, item.ID, execution.StatusCompleted); err != nil {
			return err
		}
	}
	return nil
}

func (s ExecutionService) materializeRecurringSchedules(ctx context.Context, limit int) error {
	iterations, err := s.iterationRepo.ListLiveScheduled(ctx, limit)
	if err != nil {
		return err
	}

	now := s.clock.Now().UTC()
	for _, iteration := range iterations {
		cfg, err := DecodeRecurringScheduleConfig(iteration.Schedule)
		if err != nil {
			return err
		}
		if !cfg.Enabled {
			continue
		}

		scheduledFor, err := nextScheduledTimeForDay(now, cfg)
		if err != nil {
			return err
		}
		if scheduledFor.After(now) {
			continue
		}

		existing, err := s.scheduledRepo.ListByScenario(ctx, iteration.TenantID, iteration.ScenarioID)
		if err != nil {
			return err
		}
		alreadyCreated := false
		for _, item := range existing {
			if item.ScheduledFor.Equal(scheduledFor) {
				alreadyCreated = true
				break
			}
		}
		if alreadyCreated {
			continue
		}

		body, err := json.Marshal(ScheduledExecutionRequest{
			Items:          nil,
			CandidateLimit: cfg.CandidateLimit,
		})
		if err != nil {
			return err
		}

		item := execution.ScheduledExecution{
			ID:                  s.idGen.New().String(),
			TenantID:            iteration.TenantID,
			ScenarioID:          iteration.ScenarioID,
			ScenarioIterationID: iteration.ID,
			Status:              execution.StatusPending,
			ScheduledFor:        scheduledFor,
			RequestBody:         body,
			CreatedAt:           now,
		}
		if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
			_, err := store.ScheduledExecutions().Create(ctx, item)
			return err
		}); err != nil {
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
			continue
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
