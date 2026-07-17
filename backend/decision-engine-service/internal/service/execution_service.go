package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
)

type AsyncDecisionExecutionRequest struct {
	ScenarioID     string                      `json:"scenario_id"`
	ObjectType     string                      `json:"object_type"`
	IdempotencyKey string                      `json:"idempotency_key"`
	WaitTimeoutMS  int                         `json:"wait_timeout_ms"`
	CallbackURL    string                      `json:"callback_url"`
	Items          []DecisionEvaluationRequest `json:"items"`
}

type AsyncDecisionExecutionCreateResult struct {
	Execution       execution.AsyncDecisionExecution `json:"execution"`
	CompletedInline bool                             `json:"completed_inline"`
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

type AsyncExecutionBehavior struct {
	DefaultWaitWindow     time.Duration
	MaxWaitWindow         time.Duration
	WaitPollInterval      time.Duration
	CallbackTimeout       time.Duration
	CallbackHTTPClient    *http.Client
	CallbackSigningSecret string
}

type ExecutionService struct {
	txManager             ports.TransactionManager
	idGen                 ports.IDGenerator
	clock                 ports.Clock
	scenarioRepo          ports.ScenarioRepository
	iterationRepo         ports.ScenarioIterationRepository
	tenantDataReader      ports.TenantDataReader
	scheduledRepo         ports.ScheduledExecutionRepository
	asyncRepo             ports.AsyncDecisionExecutionRepository
	outboxRepo            ports.OutboxEventRepository
	decisionSvc           DecisionService
	retryPolicy           ExecutionRetryPolicy
	asyncBehavior         AsyncExecutionBehavior
	scheduledEnqueuer     riverjobs.ScheduledExecutionEnqueuer
	asyncEnqueuer         riverjobs.AsyncDecisionExecutionEnqueuer
	asyncCallbackEnqueuer riverjobs.AsyncDecisionExecutionCallbackEnqueuer
	outboxEnqueuer        riverjobs.OutboxEventEnqueuer
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
	asyncBehavior AsyncExecutionBehavior,
	scheduledEnqueuer riverjobs.ScheduledExecutionEnqueuer,
	asyncEnqueuer riverjobs.AsyncDecisionExecutionEnqueuer,
	asyncCallbackEnqueuer riverjobs.AsyncDecisionExecutionCallbackEnqueuer,
	outboxEnqueuer riverjobs.OutboxEventEnqueuer,
) ExecutionService {
	if scheduledEnqueuer == nil {
		scheduledEnqueuer = riverjobs.NoopScheduledExecutionEnqueuer{}
	}
	if asyncEnqueuer == nil {
		asyncEnqueuer = riverjobs.NoopAsyncDecisionExecutionEnqueuer{}
	}
	if asyncCallbackEnqueuer == nil {
		asyncCallbackEnqueuer = riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{}
	}
	if outboxEnqueuer == nil {
		outboxEnqueuer = riverjobs.NoopOutboxEventEnqueuer{}
	}
	if asyncBehavior.DefaultWaitWindow <= 0 {
		asyncBehavior.DefaultWaitWindow = 300 * time.Millisecond
	}
	if asyncBehavior.MaxWaitWindow <= 0 {
		asyncBehavior.MaxWaitWindow = time.Second
	}
	if asyncBehavior.WaitPollInterval <= 0 {
		asyncBehavior.WaitPollInterval = 10 * time.Millisecond
	}
	if asyncBehavior.CallbackTimeout <= 0 {
		asyncBehavior.CallbackTimeout = 5 * time.Second
	}
	if asyncBehavior.CallbackHTTPClient == nil {
		asyncBehavior.CallbackHTTPClient = &http.Client{Timeout: asyncBehavior.CallbackTimeout}
	}
	return ExecutionService{
		txManager:             txManager,
		idGen:                 idGen,
		clock:                 clock,
		scenarioRepo:          scenarioRepo,
		iterationRepo:         iterationRepo,
		tenantDataReader:      tenantDataReader,
		scheduledRepo:         scheduledRepo,
		asyncRepo:             asyncRepo,
		outboxRepo:            outboxRepo,
		decisionSvc:           decisionSvc,
		retryPolicy:           retryPolicy,
		asyncBehavior:         asyncBehavior,
		scheduledEnqueuer:     scheduledEnqueuer,
		asyncEnqueuer:         asyncEnqueuer,
		asyncCallbackEnqueuer: asyncCallbackEnqueuer,
		outboxEnqueuer:        outboxEnqueuer,
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
		if err := s.scheduledEnqueuer.EnqueueTx(ctx, store.RawTx(), created.ID, scheduledExecutionRunAt(now, created.ScheduledFor)); err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, store.RawTx(), created.TenantID, "scheduled_execution", created.ID, "scheduled_execution.queued", map[string]any{
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
	now := s.clock.Now()
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.ScheduledExecutions().ResetForRetry(ctx, executionID, execution.StatusPending); err != nil {
			return err
		}
		return s.scheduledEnqueuer.EnqueueTx(ctx, store.RawTx(), executionID, scheduledExecutionRunAt(now, item.ScheduledFor))
	})
	if err != nil {
		return execution.ScheduledExecution{}, err
	}
	return s.scheduledRepo.GetByID(ctx, tenantID, scenarioID, executionID)
}

func (s ExecutionService) CreateAsyncDecisionExecution(ctx context.Context, tenantID string, req AsyncDecisionExecutionRequest) (AsyncDecisionExecutionCreateResult, error) {
	if err := validateAsyncCallbackURL(req.CallbackURL); err != nil {
		return AsyncDecisionExecutionCreateResult{}, err
	}
	startedAt := time.Now()
	body, err := json.Marshal(req)
	if err != nil {
		return AsyncDecisionExecutionCreateResult{}, err
	}
	now := s.clock.Now()
	item := execution.AsyncDecisionExecution{
		ID:                   s.idGen.New().String(),
		TenantID:             tenantID,
		ScenarioID:           req.ScenarioID,
		ObjectType:           req.ObjectType,
		Status:               execution.StatusQueued,
		IdempotencyKey:       req.IdempotencyKey,
		AttemptCount:         0,
		MaxAttempts:          max(1, s.retryPolicy.AsyncMaxAttempts),
		RequestBody:          body,
		CallbackURL:          req.CallbackURL,
		CallbackAttemptCount: 0,
		CallbackLastError:    "",
		CreatedAt:            now,
	}
	var created execution.AsyncDecisionExecution
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.AsyncDecisionExecutions().Create(ctx, item)
		if err != nil {
			return err
		}
		if created.ID == item.ID {
			if err := s.asyncEnqueuer.EnqueueTx(ctx, store.RawTx(), created.ID, nil); err != nil {
				return err
			}
		}
		return s.writeExecutionLifecycleEvents(ctx, store.RawTx(), created.TenantID, "async_decision_execution", created.ID, "async_decision_execution.queued", map[string]any{
			"status":          created.Status,
			"scenario_id":     created.ScenarioID,
			"object_type":     created.ObjectType,
			"attempt_count":   created.AttemptCount,
			"max_attempts":    created.MaxAttempts,
			"idempotency_key": created.IdempotencyKey,
		}, store.OutboxEvents())
	})
	if err != nil {
		return AsyncDecisionExecutionCreateResult{}, err
	}
	waited, err := s.waitForAsyncExecutionTerminalState(ctx, tenantID, created.ID, s.resolveAsyncWaitWindow(req.WaitTimeoutMS))
	if err != nil {
		return AsyncDecisionExecutionCreateResult{}, err
	}
	result := AsyncDecisionExecutionCreateResult{
		Execution:       waited,
		CompletedInline: waited.Status == execution.StatusCompleted,
	}
	if result.CompletedInline {
		slog.Default().Info("async execution completed within wait window",
			"tenant_id", tenantID,
			"execution_id", waited.ID,
			"object_type", waited.ObjectType,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"callback_enabled", waited.CallbackURL != "",
		)
	} else {
		slog.Default().Info("async execution returned before completion",
			"tenant_id", tenantID,
			"execution_id", waited.ID,
			"status", waited.Status,
			"object_type", waited.ObjectType,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"callback_enabled", waited.CallbackURL != "",
		)
	}
	return result, nil
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
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.AsyncDecisionExecutions().ResetForRetry(ctx, executionID, execution.StatusQueued); err != nil {
			return err
		}
		return s.asyncEnqueuer.EnqueueTx(ctx, store.RawTx(), executionID, nil)
	})
	if err != nil {
		return execution.AsyncDecisionExecution{}, err
	}
	return s.asyncRepo.GetByID(ctx, tenantID, executionID)
}

func (s ExecutionService) RunScheduledExecution(ctx context.Context, executionID string) error {
	item, err := s.scheduledRepo.StartAttempt(ctx, executionID)
	if err != nil {
		return err
	}
	if err := s.runScheduledExecution(ctx, item); err != nil {
		return s.handleScheduledExecutionFailure(ctx, item, err)
	}
	if err := s.scheduledRepo.UpdateStatus(ctx, item.ID, execution.StatusCompleted); err != nil {
		return err
	}
	return s.writeExecutionLifecycleEvents(ctx, nil, item.TenantID, "scheduled_execution", item.ID, "scheduled_execution.completed", map[string]any{
		"status":        execution.StatusCompleted,
		"scenario_id":   item.ScenarioID,
		"attempt_count": item.AttemptCount,
	}, s.outboxRepo)
}

func (s ExecutionService) RunAsyncDecisionExecution(ctx context.Context, executionID string) error {
	item, err := s.asyncRepo.StartAttempt(ctx, executionID)
	if err != nil {
		return err
	}
	resultBody, err := s.runAsyncExecution(ctx, item)
	if err != nil {
		return s.handleAsyncExecutionFailure(ctx, item, err)
	}
	callbackStatus := ""
	if item.CallbackURL != "" {
		callbackStatus = "pending"
	}
	completedAt := s.clock.Now()
	if err := s.asyncRepo.MarkCompleted(ctx, item.ID, resultBody, completedAt, callbackStatus); err != nil {
		return err
	}
	item.Status = execution.StatusCompleted
	item.ResultBody = resultBody
	item.CompletedAt = &completedAt
	item.CallbackStatus = callbackStatus
	if err := s.enqueueAsyncExecutionCallback(ctx, item); err != nil {
		return err
	}
	return s.writeExecutionLifecycleEvents(ctx, nil, item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.completed", map[string]any{
		"status":        execution.StatusCompleted,
		"scenario_id":   item.ScenarioID,
		"object_type":   item.ObjectType,
		"attempt_count": item.AttemptCount,
	}, s.outboxRepo)
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
				created, err := store.ScheduledExecutions().Create(ctx, item)
				if err != nil {
					return err
				}
				return s.scheduledEnqueuer.EnqueueTx(ctx, store.RawTx(), created.ID, scheduledExecutionRunAt(now, created.ScheduledFor))
			}); err != nil {
				return err
			}
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

func (s ExecutionService) runAsyncExecution(ctx context.Context, item execution.AsyncDecisionExecution) ([]byte, error) {
	var req AsyncDecisionExecutionRequest
	if err := json.Unmarshal(item.RequestBody, &req); err != nil {
		return nil, err
	}
	var result any
	for _, evalReq := range req.Items {
		if req.ScenarioID != "" {
			singleResult, err := s.decisionSvc.EvaluateScenario(ctx, item.TenantID, req.ScenarioID, evalReq)
			if err != nil {
				return nil, err
			}
			result = singleResult
			continue
		}
		multiResult, err := s.decisionSvc.EvaluateAllLiveScenarios(ctx, item.TenantID, evalReq)
		if err != nil {
			return nil, err
		}
		result = multiResult
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return body, nil
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
		return s.writeExecutionLifecycleEvents(ctx, nil, item.TenantID, "scheduled_execution", item.ID, "scheduled_execution.failed", map[string]any{
			"status":        execution.StatusFailed,
			"scenario_id":   item.ScenarioID,
			"attempt_count": item.AttemptCount,
			"max_attempts":  item.MaxAttempts,
			"last_error":    runErr.Error(),
			"failed_at":     now,
		}, s.outboxRepo)
	}
	nextAttemptAt := now.Add(s.retryDelay(s.retryPolicy.ScheduledBaseBackoff, item.AttemptCount))
	if s.txManager == nil {
		if err := s.scheduledRepo.RecordAttemptFailure(ctx, item.ID, execution.StatusPending, &nextAttemptAt, runErr.Error(), nil); err != nil {
			return err
		}
		if s.scheduledEnqueuer != nil {
			if err := s.scheduledEnqueuer.Enqueue(ctx, item.ID, &nextAttemptAt); err != nil {
				return err
			}
		}
		return s.writeExecutionLifecycleEvents(ctx, nil, item.TenantID, "scheduled_execution", item.ID, "scheduled_execution.retry_scheduled", map[string]any{
			"status":          execution.StatusPending,
			"scenario_id":     item.ScenarioID,
			"attempt_count":   item.AttemptCount,
			"max_attempts":    item.MaxAttempts,
			"last_error":      runErr.Error(),
			"next_attempt_at": nextAttemptAt,
		}, s.outboxRepo)
	}
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.ScheduledExecutions().RecordAttemptFailure(ctx, item.ID, execution.StatusPending, &nextAttemptAt, runErr.Error(), nil); err != nil {
			return err
		}
		if err := s.scheduledEnqueuer.EnqueueTx(ctx, store.RawTx(), item.ID, &nextAttemptAt); err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, store.RawTx(), item.TenantID, "scheduled_execution", item.ID, "scheduled_execution.retry_scheduled", map[string]any{
			"status":          execution.StatusPending,
			"scenario_id":     item.ScenarioID,
			"attempt_count":   item.AttemptCount,
			"max_attempts":    item.MaxAttempts,
			"last_error":      runErr.Error(),
			"next_attempt_at": nextAttemptAt,
		}, store.OutboxEvents())
	})
}

func (s ExecutionService) handleAsyncExecutionFailure(ctx context.Context, item execution.AsyncDecisionExecution, runErr error) error {
	now := s.clock.Now()
	if item.AttemptCount >= max(1, item.MaxAttempts) {
		if err := s.asyncRepo.RecordAttemptFailure(ctx, item.ID, execution.StatusFailed, nil, runErr.Error(), &now); err != nil {
			return err
		}
		item.Status = execution.StatusFailed
		item.LastError = runErr.Error()
		item.FailedAt = &now
		if err := s.enqueueAsyncExecutionCallback(ctx, item); err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, nil, item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.failed", map[string]any{
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
	if s.txManager == nil {
		if err := s.asyncRepo.RecordAttemptFailure(ctx, item.ID, execution.StatusQueued, &nextAttemptAt, runErr.Error(), nil); err != nil {
			return err
		}
		if s.asyncEnqueuer != nil {
			if err := s.asyncEnqueuer.Enqueue(ctx, item.ID, &nextAttemptAt); err != nil {
				return err
			}
		}
		return s.writeExecutionLifecycleEvents(ctx, nil, item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.retry_scheduled", map[string]any{
			"status":          execution.StatusQueued,
			"scenario_id":     item.ScenarioID,
			"object_type":     item.ObjectType,
			"attempt_count":   item.AttemptCount,
			"max_attempts":    item.MaxAttempts,
			"last_error":      runErr.Error(),
			"next_attempt_at": nextAttemptAt,
		}, s.outboxRepo)
	}
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.AsyncDecisionExecutions().RecordAttemptFailure(ctx, item.ID, execution.StatusQueued, &nextAttemptAt, runErr.Error(), nil); err != nil {
			return err
		}
		if err := s.asyncEnqueuer.EnqueueTx(ctx, store.RawTx(), item.ID, &nextAttemptAt); err != nil {
			return err
		}
		return s.writeExecutionLifecycleEvents(ctx, store.RawTx(), item.TenantID, "async_decision_execution", item.ID, "async_decision_execution.retry_scheduled", map[string]any{
			"status":          execution.StatusQueued,
			"scenario_id":     item.ScenarioID,
			"object_type":     item.ObjectType,
			"attempt_count":   item.AttemptCount,
			"max_attempts":    item.MaxAttempts,
			"last_error":      runErr.Error(),
			"next_attempt_at": nextAttemptAt,
		}, store.OutboxEvents())
	})
}

func (s ExecutionService) resolveAsyncWaitWindow(waitTimeoutMS int) time.Duration {
	if waitTimeoutMS <= 0 {
		return s.asyncBehavior.DefaultWaitWindow
	}
	waitWindow := time.Duration(waitTimeoutMS) * time.Millisecond
	if waitWindow > s.asyncBehavior.MaxWaitWindow {
		return s.asyncBehavior.MaxWaitWindow
	}
	return waitWindow
}

func (s ExecutionService) waitForAsyncExecutionTerminalState(ctx context.Context, tenantID, executionID string, waitWindow time.Duration) (execution.AsyncDecisionExecution, error) {
	if waitWindow <= 0 {
		return s.asyncRepo.GetByID(ctx, tenantID, executionID)
	}
	deadline := s.clock.Now().Add(waitWindow)
	pollInterval := s.asyncBehavior.WaitPollInterval
	for {
		item, err := s.asyncRepo.GetByID(ctx, tenantID, executionID)
		if err != nil {
			return execution.AsyncDecisionExecution{}, err
		}
		if item.Status == execution.StatusCompleted || item.Status == execution.StatusFailed {
			return item, nil
		}
		if s.clock.Now().After(deadline) || s.clock.Now().Equal(deadline) {
			return item, nil
		}
		select {
		case <-ctx.Done():
			return execution.AsyncDecisionExecution{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (s ExecutionService) enqueueAsyncExecutionCallback(ctx context.Context, item execution.AsyncDecisionExecution) error {
	if item.CallbackURL == "" {
		return nil
	}
	return s.asyncCallbackEnqueuer.Enqueue(ctx, item.TenantID, item.ID, nil)
}

func validateAsyncCallbackURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("callback_url is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("callback_url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("callback_url must include a host")
	}
	return nil
}

func (s ExecutionService) DeliverAsyncExecutionCallback(ctx context.Context, tenantID, executionID string) error {
	item, err := s.asyncRepo.GetByID(ctx, tenantID, executionID)
	if err != nil {
		return err
	}
	if item.CallbackURL == "" || item.CallbackStatus == "sent" {
		return nil
	}
	return s.deliverAsyncExecutionCallback(ctx, item)
}

func (s ExecutionService) deliverAsyncExecutionCallback(ctx context.Context, item execution.AsyncDecisionExecution) error {
	if item.CallbackURL == "" {
		return nil
	}
	eventType := "completed"
	failureMessage := ""
	if item.Status == execution.StatusFailed {
		eventType = "failed"
		failureMessage = item.LastError
	}
	payload := map[string]any{
		"execution_id": item.ID,
		"tenant_id":    item.TenantID,
		"scenario_id":  item.ScenarioID,
		"object_type":  item.ObjectType,
		"status":       item.Status,
		"event":        eventType,
	}
	if len(item.ResultBody) > 0 {
		payload["result_body"] = json.RawMessage(item.ResultBody)
	}
	if failureMessage != "" {
		payload["error"] = failureMessage
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, s.asyncBehavior.CallbackTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, item.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Async-Execution-ID", item.ID)
	timestamp := s.clock.Now().UTC().Format(time.RFC3339)
	req.Header.Set("X-Async-Execution-Timestamp", timestamp)
	if secret := s.asyncBehavior.CallbackSigningSecret; secret != "" {
		signature := signAsyncExecutionCallback(secret, timestamp, body)
		req.Header.Set("X-Async-Execution-Signature", "sha256="+signature)
	}
	resp, err := s.asyncBehavior.CallbackHTTPClient.Do(req)
	attemptCount := item.CallbackAttemptCount + 1
	if err != nil {
		slog.Default().Warn("async execution callback delivery failed",
			"tenant_id", item.TenantID,
			"execution_id", item.ID,
			"attempt_count", attemptCount,
			"error", err,
		)
		return s.asyncRepo.UpdateCallbackDelivery(ctx, item.ID, "failed", attemptCount, err.Error(), nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Default().Warn("async execution callback returned non-success status",
			"tenant_id", item.TenantID,
			"execution_id", item.ID,
			"attempt_count", attemptCount,
			"status_code", resp.StatusCode,
		)
		return s.asyncRepo.UpdateCallbackDelivery(ctx, item.ID, "failed", attemptCount, fmt.Sprintf("callback returned status %d", resp.StatusCode), nil)
	}
	sentAt := s.clock.Now()
	slog.Default().Info("async execution callback delivered",
		"tenant_id", item.TenantID,
		"execution_id", item.ID,
		"attempt_count", attemptCount,
	)
	return s.asyncRepo.UpdateCallbackDelivery(ctx, item.ID, "sent", attemptCount, "", &sentAt)
}

func signAsyncExecutionCallback(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
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

func scheduledExecutionRunAt(now, scheduledFor time.Time) *time.Time {
	if scheduledFor.After(now) {
		at := scheduledFor
		return &at
	}
	return nil
}

func (s ExecutionService) writeExecutionLifecycleEvents(
	ctx context.Context,
	tx pgx.Tx,
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
	events, err := repo.CreateMany(ctx, []integration.OutboxEvent{{
		ID:            s.idGen.New().String(),
		TenantID:      tenantID,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       body,
		Status:        integration.OutboxStatusPending,
		CreatedAt:     s.clock.Now(),
	}})
	if err != nil {
		return err
	}
	for _, item := range events {
		if tx != nil {
			if err := s.outboxEnqueuer.EnqueueTx(ctx, tx, item.TenantID, item.ID, nil); err != nil {
				return err
			}
			continue
		}
		if err := s.outboxEnqueuer.Enqueue(ctx, item.TenantID, item.ID, nil); err != nil {
			return err
		}
	}
	return nil
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
