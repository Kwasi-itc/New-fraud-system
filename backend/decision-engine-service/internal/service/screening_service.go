package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type ScreeningService struct {
	txManager    ports.TransactionManager
	idGen        ports.IDGenerator
	clock        ports.Clock
	scenarioRepo ports.ScenarioRepository
	configRepo   ports.ScreeningConfigRepository
	execRepo     ports.ScreeningExecutionRepository
}

func NewScreeningService(txManager ports.TransactionManager, idGen ports.IDGenerator, clock ports.Clock, scenarioRepo ports.ScenarioRepository, configRepo ports.ScreeningConfigRepository, execRepo ports.ScreeningExecutionRepository) ScreeningService {
	return ScreeningService{txManager: txManager, idGen: idGen, clock: clock, scenarioRepo: scenarioRepo, configRepo: configRepo, execRepo: execRepo}
}

func (s ScreeningService) CreateConfig(ctx context.Context, tenantID, scenarioID, name string, allowedOutcomes []string, provider string, configJSON json.RawMessage, active bool) (screening.Config, error) {
	if _, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID); err != nil {
		return screening.Config{}, err
	}
	now := s.clock.Now()
	item := screening.Config{
		ID:              s.idGen.New().String(),
		TenantID:        tenantID,
		ScenarioID:      scenarioID,
		Name:            name,
		AllowedOutcomes: allowedOutcomes,
		Provider:        provider,
		ConfigJSON:      configJSON,
		Active:          active,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := item.Validate(); err != nil {
		return screening.Config{}, err
	}
	var created screening.Config
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.ScreeningConfigs().Create(ctx, item)
		return err
	})
	return created, err
}

func (s ScreeningService) ListConfigsByScenario(ctx context.Context, tenantID, scenarioID string) ([]screening.Config, error) {
	return s.configRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s ScreeningService) GetConfig(ctx context.Context, tenantID, scenarioID, configID string) (screening.Config, error) {
	return s.configRepo.GetByID(ctx, tenantID, scenarioID, configID)
}

func (s ScreeningService) UpdateConfig(ctx context.Context, tenantID, scenarioID, configID, name string, allowedOutcomes []string, provider string, configJSON json.RawMessage, active bool) (screening.Config, error) {
	current, err := s.configRepo.GetByID(ctx, tenantID, scenarioID, configID)
	if err != nil {
		return screening.Config{}, err
	}
	current.Name = name
	current.AllowedOutcomes = allowedOutcomes
	current.Provider = provider
	current.ConfigJSON = configJSON
	current.Active = active
	current.UpdatedAt = s.clock.Now()
	if err := current.Validate(); err != nil {
		return screening.Config{}, err
	}
	var updated screening.Config
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScreeningConfigs().Update(ctx, current)
		return runErr
	})
	return updated, err
}

func (s ScreeningService) DeleteConfig(ctx context.Context, tenantID, scenarioID, configID string) error {
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.ScreeningConfigs().Delete(ctx, tenantID, scenarioID, configID)
	})
}

func (s ScreeningService) ListExecutionsByDecision(ctx context.Context, tenantID, decisionID string) ([]screening.Execution, error) {
	return s.execRepo.ListByDecision(ctx, tenantID, decisionID)
}

func (s ScreeningService) GetExecution(ctx context.Context, tenantID, executionID string) (screening.Execution, error) {
	return s.execRepo.GetByID(ctx, tenantID, executionID)
}

func (s ScreeningService) UpdateExecutionStatus(ctx context.Context, tenantID, executionID, status string, providerReference *string, responseJSON *json.RawMessage, lastError *string) (screening.Execution, error) {
	item, err := s.execRepo.GetByID(ctx, tenantID, executionID)
	if err != nil {
		return screening.Execution{}, err
	}
	nextStatus, err := parseScreeningExecutionStatus(status)
	if err != nil {
		return screening.Execution{}, err
	}
	now := s.clock.Now()
	item.Status = nextStatus
	item.UpdatedAt = now
	if providerReference != nil {
		item.ProviderReference = *providerReference
	}
	if responseJSON != nil {
		item.ResponseJSON = *responseJSON
	}
	if lastError != nil {
		item.LastError = *lastError
	}
	switch nextStatus {
	case screening.ExecutionStatusSent:
		item.SentAt = &now
		item.FailedAt = nil
	case screening.ExecutionStatusCompleted:
		item.CompletedAt = &now
		item.FailedAt = nil
		item.LastError = ""
	case screening.ExecutionStatusFailed:
		item.FailedAt = &now
		item.CompletedAt = nil
	case screening.ExecutionStatusPending, screening.ExecutionStatusQueued:
		item.CompletedAt = nil
		item.FailedAt = nil
	}
	var updated screening.Execution
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScreeningExecutions().Update(ctx, item)
		return runErr
	})
	return updated, err
}

func (s ScreeningService) RetryExecution(ctx context.Context, tenantID, executionID string) (screening.Execution, error) {
	item, err := s.execRepo.GetByID(ctx, tenantID, executionID)
	if err != nil {
		return screening.Execution{}, err
	}
	if item.Status != screening.ExecutionStatusFailed && item.Status != screening.ExecutionStatusSent {
		return screening.Execution{}, fmt.Errorf("screening execution status %q cannot be retried", item.Status)
	}
	now := s.clock.Now()
	item.Status = screening.ExecutionStatusPending
	item.ProviderReference = ""
	item.ResponseJSON = json.RawMessage(`{}`)
	item.LastError = ""
	item.UpdatedAt = now
	item.SentAt = nil
	item.CompletedAt = nil
	item.FailedAt = nil
	var updated screening.Execution
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScreeningExecutions().Update(ctx, item)
		return runErr
	})
	return updated, err
}

func parseScreeningExecutionStatus(raw string) (screening.ExecutionStatus, error) {
	switch screening.ExecutionStatus(raw) {
	case screening.ExecutionStatusPending, screening.ExecutionStatusQueued, screening.ExecutionStatusSent, screening.ExecutionStatusCompleted, screening.ExecutionStatusFailed:
		return screening.ExecutionStatus(raw), nil
	default:
		return "", fmt.Errorf("invalid screening execution status %q", raw)
	}
}
