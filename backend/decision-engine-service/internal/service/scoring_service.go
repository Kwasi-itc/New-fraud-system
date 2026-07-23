package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
)

type ScoringService struct {
	txManager        ports.TransactionManager
	idGen            ports.IDGenerator
	clock            ports.Clock
	scenarioRepo     ports.ScenarioRepository
	configRepo       ports.ScoringConfigRepository
	requestRepo      ports.ScoringRequestRepository
	enqueuer         riverjobs.ScoringRequestEnqueuer
	cacheInvalidator DecisionMetadataCacheInvalidator
}

func NewScoringService(txManager ports.TransactionManager, idGen ports.IDGenerator, clock ports.Clock, scenarioRepo ports.ScenarioRepository, configRepo ports.ScoringConfigRepository, requestRepo ports.ScoringRequestRepository, enqueuer riverjobs.ScoringRequestEnqueuer) ScoringService {
	if enqueuer == nil {
		enqueuer = riverjobs.NoopScoringRequestEnqueuer{}
	}
	return ScoringService{txManager: txManager, idGen: idGen, clock: clock, scenarioRepo: scenarioRepo, configRepo: configRepo, requestRepo: requestRepo, enqueuer: enqueuer}
}

func (s *ScoringService) SetCacheInvalidator(invalidator DecisionMetadataCacheInvalidator) {
	s.cacheInvalidator = invalidator
}

func (s ScoringService) CreateConfig(ctx context.Context, tenantID, scenarioID, name string, allowedOutcomes []string, rulesetRef string, configJSON json.RawMessage, active bool) (scoring.Config, error) {
	if _, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID); err != nil {
		return scoring.Config{}, err
	}
	now := s.clock.Now()
	item := scoring.Config{
		ID:              s.idGen.New().String(),
		TenantID:        tenantID,
		ScenarioID:      scenarioID,
		Name:            name,
		AllowedOutcomes: allowedOutcomes,
		RulesetRef:      rulesetRef,
		ConfigJSON:      configJSON,
		Active:          active,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := item.Validate(); err != nil {
		return scoring.Config{}, err
	}
	var created scoring.Config
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.ScoringConfigs().Create(ctx, item)
		return err
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateActiveScoringConfigs(ctx, tenantID, scenarioID)
	}
	return created, err
}

func (s ScoringService) ListConfigsByScenario(ctx context.Context, tenantID, scenarioID string) ([]scoring.Config, error) {
	return s.configRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s ScoringService) GetConfig(ctx context.Context, tenantID, scenarioID, configID string) (scoring.Config, error) {
	return s.configRepo.GetByID(ctx, tenantID, scenarioID, configID)
}

func (s ScoringService) UpdateConfig(ctx context.Context, tenantID, scenarioID, configID, name string, allowedOutcomes []string, rulesetRef string, configJSON json.RawMessage, active bool) (scoring.Config, error) {
	current, err := s.configRepo.GetByID(ctx, tenantID, scenarioID, configID)
	if err != nil {
		return scoring.Config{}, err
	}
	current.Name = name
	current.AllowedOutcomes = allowedOutcomes
	current.RulesetRef = rulesetRef
	current.ConfigJSON = configJSON
	current.Active = active
	current.UpdatedAt = s.clock.Now()
	if err := current.Validate(); err != nil {
		return scoring.Config{}, err
	}
	var updated scoring.Config
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScoringConfigs().Update(ctx, current)
		return runErr
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateActiveScoringConfigs(ctx, tenantID, scenarioID)
	}
	return updated, err
}

func (s ScoringService) DeleteConfig(ctx context.Context, tenantID, scenarioID, configID string) error {
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.ScoringConfigs().Delete(ctx, tenantID, scenarioID, configID)
	})
	if err == nil && s.cacheInvalidator != nil {
		s.cacheInvalidator.InvalidateActiveScoringConfigs(ctx, tenantID, scenarioID)
	}
	return err
}

func (s ScoringService) ListRequestsByDecision(ctx context.Context, tenantID, decisionID string) ([]scoring.Request, error) {
	return s.requestRepo.ListByDecision(ctx, tenantID, decisionID)
}

func (s ScoringService) GetRequest(ctx context.Context, tenantID, requestID string) (scoring.Request, error) {
	return s.requestRepo.GetByID(ctx, tenantID, requestID)
}

func (s ScoringService) UpdateRequestStatus(ctx context.Context, tenantID, requestID, status string, providerReference *string, responseJSON *json.RawMessage, lastError *string) (scoring.Request, error) {
	item, err := s.requestRepo.GetByID(ctx, tenantID, requestID)
	if err != nil {
		return scoring.Request{}, err
	}
	nextStatus, err := parseScoringRequestStatus(status)
	if err != nil {
		return scoring.Request{}, err
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
	case scoring.RequestStatusSent:
		item.SentAt = &now
		item.FailedAt = nil
	case scoring.RequestStatusCompleted:
		item.CompletedAt = &now
		item.FailedAt = nil
		item.LastError = ""
	case scoring.RequestStatusFailed:
		item.FailedAt = &now
		item.CompletedAt = nil
	case scoring.RequestStatusPending, scoring.RequestStatusQueued:
		item.CompletedAt = nil
		item.FailedAt = nil
	}
	var updated scoring.Request
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScoringRequests().Update(ctx, item)
		if runErr != nil {
			return runErr
		}
		return s.enqueuer.EnqueueTx(ctx, store.RawTx(), updated.TenantID, updated.ID, nil)
	})
	return updated, err
}

func (s ScoringService) RetryRequest(ctx context.Context, tenantID, requestID string) (scoring.Request, error) {
	item, err := s.requestRepo.GetByID(ctx, tenantID, requestID)
	if err != nil {
		return scoring.Request{}, err
	}
	if item.Status != scoring.RequestStatusFailed && item.Status != scoring.RequestStatusSent {
		return scoring.Request{}, fmt.Errorf("scoring request status %q cannot be retried", item.Status)
	}
	now := s.clock.Now()
	item.Status = scoring.RequestStatusPending
	item.ProviderReference = ""
	item.ResponseJSON = json.RawMessage(`{}`)
	item.LastError = ""
	item.UpdatedAt = now
	item.SentAt = nil
	item.CompletedAt = nil
	item.FailedAt = nil
	var updated scoring.Request
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var runErr error
		updated, runErr = store.ScoringRequests().Update(ctx, item)
		return runErr
	})
	return updated, err
}

func parseScoringRequestStatus(raw string) (scoring.RequestStatus, error) {
	switch scoring.RequestStatus(raw) {
	case scoring.RequestStatusPending, scoring.RequestStatusQueued, scoring.RequestStatusSent, scoring.RequestStatusCompleted, scoring.RequestStatusFailed:
		return scoring.RequestStatus(raw), nil
	default:
		return "", fmt.Errorf("invalid scoring request status %q", raw)
	}
}
