package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type AsyncDecisionExecutionRequest struct {
	ScenarioID     string                      `json:"scenario_id"`
	ObjectType     string                      `json:"object_type"`
	IdempotencyKey string                      `json:"idempotency_key"`
	Items          []DecisionEvaluationRequest `json:"items"`
}

type ScheduledExecutionRequest struct {
	Items          []DecisionEvaluationRequest `json:"items"`
	CandidateLimit int                         `json:"candidate_limit"`
	IdempotencyKey string                      `json:"idempotency_key"`
}

type ExecutionStatusSummary struct {
	Pending   int `json:"pending"`
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type ExecutionListFilter struct {
	Status          execution.Status
	MinAttemptCount int
	Limit           int
}

type ExecutionRetryPolicy struct {
	ScheduledMaxAttempts int
	ScheduledBaseBackoff time.Duration
	AsyncMaxAttempts     int
	AsyncBaseBackoff     time.Duration
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
	outboxRepo       ports.OutboxEventRepository
	decisionSvc      DecisionService
	retryPolicy      ExecutionRetryPolicy
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
	outboxRepo ports.OutboxEventRepository,
	decisionSvc DecisionService,
	retryPolicy ExecutionRetryPolicy,
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
		outboxRepo:       outboxRepo,
		decisionSvc:      decisionSvc,
		retryPolicy:      retryPolicy,
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
	cfg, err := DecodeRecurringScheduleConfig(iteration.Schedule)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	items, err := s.scheduledRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	return hydrateRecurringSchedule(cfg, s.clock.Now(), items)
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
	items, err := s.scheduledRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return RecurringScheduleConfig{}, err
	}
	return hydrateRecurringSchedule(normalized, s.clock.Now(), items)
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
		Source:              execution.SourceManual,
		Status:              execution.StatusPending,
		IdempotencyKey:      req.IdempotencyKey,
		AttemptCount:        0,
		MaxAttempts:         max(1, s.retryPolicy.ScheduledMaxAttempts),
		ScheduledFor:        scheduledFor,
		RequestBody:         body,
		LastError:           "",
		CreatedAt:           now,
	}
	var created execution.ScheduledExecution
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.ScheduledExecutions().Create(ctx, item)
		if err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, created.TenantID, "scheduled_execution", created.ID, "scheduled_execution.queued", map[string]any{
			"status":          created.Status,
			"scenario_id":     created.ScenarioID,
			"scheduled_for":   created.ScheduledFor,
			"attempt_count":   created.AttemptCount,
			"max_attempts":    created.MaxAttempts,
			"idempotency_key": created.IdempotencyKey,
		}, store.OutboxEvents())
	})
	return created, err
}

func (s ExecutionService) ListScheduledExecutionsByScenario(ctx context.Context, tenantID, scenarioID string) ([]execution.ScheduledExecution, error) {
	return s.scheduledRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s ExecutionService) ListScheduledExecutionsByScenarioFiltered(ctx context.Context, tenantID, scenarioID string, filter ExecutionListFilter) ([]execution.ScheduledExecution, error) {
	items, err := s.scheduledRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	return filterScheduledExecutions(items, filter), nil
}

func (s ExecutionService) GetScheduledExecutionByID(ctx context.Context, tenantID, scenarioID, executionID string) (execution.ScheduledExecution, error) {
	return s.scheduledRepo.GetByID(ctx, tenantID, scenarioID, executionID)
}

func (s ExecutionService) GetScheduledExecutionStatusSummary(ctx context.Context, tenantID, scenarioID string) (ExecutionStatusSummary, error) {
	counts, err := s.scheduledRepo.CountByStatus(ctx, tenantID, scenarioID)
	if err != nil {
		return ExecutionStatusSummary{}, err
	}
	return adaptExecutionStatusSummary(counts), nil
}

func (s ExecutionService) RetryScheduledExecution(ctx context.Context, tenantID, scenarioID, executionID string) (execution.ScheduledExecution, error) {
	item, err := s.scheduledRepo.GetByID(ctx, tenantID, scenarioID, executionID)
	if err != nil {
		return execution.ScheduledExecution{}, err
	}
	if item.Status != execution.StatusFailed {
		return execution.ScheduledExecution{}, fmt.Errorf("scheduled execution must be in failed status to retry")
	}
	if err := s.scheduledRepo.ResetForRetry(ctx, executionID, execution.StatusPending); err != nil {
		return execution.ScheduledExecution{}, err
	}
	return s.scheduledRepo.GetByID(ctx, tenantID, scenarioID, executionID)
}

func (s ExecutionService) CreateAsyncDecisionExecution(ctx context.Context, tenantID string, req AsyncDecisionExecutionRequest) (execution.AsyncDecisionExecution, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return execution.AsyncDecisionExecution{}, err
	}
	now := s.clock.Now()
	item := execution.AsyncDecisionExecution{
		ID:             s.idGen.New().String(),
		TenantID:       tenantID,
		ScenarioID:     req.ScenarioID,
		ObjectType:     req.ObjectType,
		Status:         execution.StatusQueued,
		IdempotencyKey: req.IdempotencyKey,
		AttemptCount:   0,
		MaxAttempts:    max(1, s.retryPolicy.AsyncMaxAttempts),
		RequestBody:    body,
		LastError:      "",
		CreatedAt:      now,
	}
	var created execution.AsyncDecisionExecution
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.AsyncDecisionExecutions().Create(ctx, item)
		if err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, created.TenantID, "async_decision_execution", created.ID, "async_decision_execution.queued", map[string]any{
			"status":          created.Status,
			"scenario_id":     created.ScenarioID,
			"object_type":     created.ObjectType,
			"attempt_count":   created.AttemptCount,
			"max_attempts":    created.MaxAttempts,
			"idempotency_key": created.IdempotencyKey,
		}, store.OutboxEvents())
	})
	return created, err
}

func (s ExecutionService) ListAsyncDecisionExecutionsByTenant(ctx context.Context, tenantID string) ([]execution.AsyncDecisionExecution, error) {
	return s.asyncRepo.ListByTenant(ctx, tenantID)
}

func (s ExecutionService) ListAsyncDecisionExecutionsByTenantFiltered(ctx context.Context, tenantID string, filter ExecutionListFilter) ([]execution.AsyncDecisionExecution, error) {
	items, err := s.asyncRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return filterAsyncExecutions(items, filter), nil
}

func (s ExecutionService) GetAsyncDecisionExecutionByID(ctx context.Context, tenantID, executionID string) (execution.AsyncDecisionExecution, error) {
	return s.asyncRepo.GetByID(ctx, tenantID, executionID)
}

func (s ExecutionService) GetAsyncDecisionExecutionStatusSummary(ctx context.Context, tenantID string) (ExecutionStatusSummary, error) {
	counts, err := s.asyncRepo.CountByStatus(ctx, tenantID)
	if err != nil {
		return ExecutionStatusSummary{}, err
	}
	return adaptExecutionStatusSummary(counts), nil
}

func (s ExecutionService) RetryAsyncDecisionExecution(ctx context.Context, tenantID, executionID string) (execution.AsyncDecisionExecution, error) {
	item, err := s.asyncRepo.GetByID(ctx, tenantID, executionID)
	if err != nil {
		return execution.AsyncDecisionExecution{}, err
	}
	if item.Status != execution.StatusFailed {
		return execution.AsyncDecisionExecution{}, fmt.Errorf("async decision execution must be in failed status to retry")
	}
	if err := s.asyncRepo.ResetForRetry(ctx, executionID, execution.StatusQueued); err != nil {
		return execution.AsyncDecisionExecution{}, err
	}
	return s.asyncRepo.GetByID(ctx, tenantID, executionID)
}

func (s ExecutionService) ProcessDueScheduledExecutions(ctx context.Context, limit int) error {
	if err := s.materializeRecurringSchedules(ctx, limit); err != nil {
		return err
	}

	var items []execution.ScheduledExecution
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var claimErr error
		items, claimErr = store.ScheduledExecutions().ClaimDue(ctx, s.clock.Now(), limit)
		return claimErr
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.runScheduledExecution(ctx, item); err != nil {
			_ = s.handleScheduledExecutionFailure(ctx, item, err)
			continue
		}
		if err := s.scheduledRepo.UpdateStatus(ctx, item.ID, execution.StatusCompleted); err != nil {
			return err
		}
		_ = s.writeExecutionLifecycleEvents(ctx, item.TenantID, "scheduled_execution", item.ID, "scheduled_execution.completed", map[string]any{
			"status":        execution.StatusCompleted,
			"scenario_id":   item.ScenarioID,
			"attempt_count": item.AttemptCount,
		}, s.outboxRepo)
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

		existing, err := s.scheduledRepo.ListByScenario(ctx, iteration.TenantID, iteration.ScenarioID)
		if err != nil {
			return err
		}
		dueTimes, err := dueRecurringScheduleTimes(now, cfg, existing, limit)
		if err != nil {
			return err
		}
		if len(dueTimes) == 0 {
			continue
		}

		body, err := json.Marshal(ScheduledExecutionRequest{
			Items:          nil,
			CandidateLimit: cfg.CandidateLimit,
		})
		if err != nil {
			return err
		}

		for _, scheduledFor := range dueTimes {
			item := execution.ScheduledExecution{
				ID:                  s.idGen.New().String(),
				TenantID:            iteration.TenantID,
				ScenarioID:          iteration.ScenarioID,
				ScenarioIterationID: iteration.ID,
				Source:              execution.SourceRecurring,
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
	}

	return nil
}

func (s ExecutionService) ProcessQueuedAsyncExecutions(ctx context.Context, limit int) error {
	var items []execution.AsyncDecisionExecution
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var claimErr error
		items, claimErr = store.AsyncDecisionExecutions().ClaimQueued(ctx, limit)
		return claimErr
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.runAsyncExecution(ctx, item); err != nil {
			_ = s.handleAsyncExecutionFailure(ctx, item, err)
			continue
		}
		if err := s.asyncRepo.UpdateStatus(ctx, item.ID, execution.StatusCompleted); err != nil {
			return err
		}
		_ = s.writeExecutionLifecycleEvents(ctx, item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.completed", map[string]any{
			"status":        execution.StatusCompleted,
			"scenario_id":   item.ScenarioID,
			"object_type":   item.ObjectType,
			"attempt_count": item.AttemptCount,
		}, s.outboxRepo)
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

func adaptExecutionStatusSummary(counts map[execution.Status]int) ExecutionStatusSummary {
	return ExecutionStatusSummary{
		Pending:   counts[execution.StatusPending],
		Queued:    counts[execution.StatusQueued],
		Running:   counts[execution.StatusRunning],
		Completed: counts[execution.StatusCompleted],
		Failed:    counts[execution.StatusFailed],
	}
}

func (s ExecutionService) handleScheduledExecutionFailure(ctx context.Context, item execution.ScheduledExecution, runErr error) error {
	now := s.clock.Now()
	if item.AttemptCount >= max(1, item.MaxAttempts) {
		if err := s.scheduledRepo.RecordAttemptFailure(ctx, item.ID, execution.StatusFailed, nil, runErr.Error(), &now); err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, item.TenantID, "scheduled_execution", item.ID, "scheduled_execution.failed", map[string]any{
			"status":        execution.StatusFailed,
			"scenario_id":   item.ScenarioID,
			"attempt_count": item.AttemptCount,
			"max_attempts":  item.MaxAttempts,
			"last_error":    runErr.Error(),
			"failed_at":     now,
		}, s.outboxRepo)
	}
	nextAttemptAt := now.Add(s.retryDelay(s.retryPolicy.ScheduledBaseBackoff, item.AttemptCount))
	if err := s.scheduledRepo.RecordAttemptFailure(ctx, item.ID, execution.StatusPending, &nextAttemptAt, runErr.Error(), nil); err != nil {
		return err
	}
	return s.writeExecutionLifecycleEvents(ctx, item.TenantID, "scheduled_execution", item.ID, "scheduled_execution.retry_scheduled", map[string]any{
		"status":          execution.StatusPending,
		"scenario_id":     item.ScenarioID,
		"attempt_count":   item.AttemptCount,
		"max_attempts":    item.MaxAttempts,
		"last_error":      runErr.Error(),
		"next_attempt_at": nextAttemptAt,
	}, s.outboxRepo)
}

func (s ExecutionService) handleAsyncExecutionFailure(ctx context.Context, item execution.AsyncDecisionExecution, runErr error) error {
	now := s.clock.Now()
	if item.AttemptCount >= max(1, item.MaxAttempts) {
		if err := s.asyncRepo.RecordAttemptFailure(ctx, item.ID, execution.StatusFailed, nil, runErr.Error(), &now); err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.failed", map[string]any{
			"status":        execution.StatusFailed,
			"scenario_id":   item.ScenarioID,
			"object_type":   item.ObjectType,
			"attempt_count": item.AttemptCount,
			"max_attempts":  item.MaxAttempts,
			"last_error":    runErr.Error(),
			"failed_at":     now,
		}, s.outboxRepo)
	}
	nextAttemptAt := now.Add(s.retryDelay(s.retryPolicy.AsyncBaseBackoff, item.AttemptCount))
	if err := s.asyncRepo.RecordAttemptFailure(ctx, item.ID, execution.StatusQueued, &nextAttemptAt, runErr.Error(), nil); err != nil {
		return err
	}
	return s.writeExecutionLifecycleEvents(ctx, item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.retry_scheduled", map[string]any{
		"status":          execution.StatusQueued,
		"scenario_id":     item.ScenarioID,
		"object_type":     item.ObjectType,
		"attempt_count":   item.AttemptCount,
		"max_attempts":    item.MaxAttempts,
		"last_error":      runErr.Error(),
		"next_attempt_at": nextAttemptAt,
	}, s.outboxRepo)
}

func (s ExecutionService) retryDelay(base time.Duration, attemptCount int) time.Duration {
	if base <= 0 {
		base = 30 * time.Second
	}
	if attemptCount <= 1 {
		return base
	}
	shift := min(attemptCount-1, 10)
	return time.Duration(float64(base) * math.Pow(2, float64(shift)))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s ExecutionService) writeExecutionLifecycleEvents(
	ctx context.Context,
	tenantID, aggregateType, aggregateID, eventType string,
	payload map[string]any,
	repo ports.OutboxEventRepository,
) error {
	if repo == nil {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = repo.CreateMany(ctx, []integration.OutboxEvent{{
		ID:            s.idGen.New().String(),
		TenantID:      tenantID,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       body,
		Status:        integration.OutboxStatusPending,
		CreatedAt:     s.clock.Now(),
	}})
	return err
}

func filterScheduledExecutions(items []execution.ScheduledExecution, filter ExecutionListFilter) []execution.ScheduledExecution {
	out := make([]execution.ScheduledExecution, 0, len(items))
	for _, item := range items {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if item.AttemptCount < filter.MinAttemptCount {
			continue
		}
		out = append(out, item)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out
}

func filterAsyncExecutions(items []execution.AsyncDecisionExecution, filter ExecutionListFilter) []execution.AsyncDecisionExecution {
	out := make([]execution.AsyncDecisionExecution, 0, len(items))
	for _, item := range items {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if item.AttemptCount < filter.MinAttemptCount {
			continue
		}
		out = append(out, item)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out
}
