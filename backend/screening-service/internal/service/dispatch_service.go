package service

import (
	"context"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

type DispatchService struct {
	txManager         ports.TransactionManager
	clock             ports.Clock
	screeningRepo     ports.ScreeningRepository
	matchRepo         ports.ScreeningMatchRepository
	provider          ports.ScreeningProvider
	decisionPublisher ports.DecisionEnginePublisher
}

func NewDispatchService(
	txManager ports.TransactionManager,
	clock ports.Clock,
	screeningRepo ports.ScreeningRepository,
	matchRepo ports.ScreeningMatchRepository,
	provider ports.ScreeningProvider,
	decisionPublisher ports.DecisionEnginePublisher,
) DispatchService {
	return DispatchService{
		txManager:         txManager,
		clock:             clock,
		screeningRepo:     screeningRepo,
		matchRepo:         matchRepo,
		provider:          provider,
		decisionPublisher: decisionPublisher,
	}
}

func (s DispatchService) ProcessPendingScreenings(ctx context.Context, limit int) error {
	items, err := s.screeningRepo.ListByStatus(ctx, screening.StatusPending, limit)
	if err != nil {
		return err
	}

	for _, item := range items {
		if err := s.processScreening(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (s DispatchService) RunScreening(ctx context.Context, tenantID, screeningID string) error {
	item, err := s.screeningRepo.GetByID(ctx, tenantID, screeningID)
	if err != nil {
		return err
	}
	if item.Status != screening.StatusPending {
		return nil
	}
	return s.processScreening(ctx, item)
}

func (s DispatchService) processScreening(ctx context.Context, item screening.Screening) error {
	now := s.clock.Now()
	item.Status = screening.StatusProcessing
	item.UpdatedAt = now
	item.SentAt = &now

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		_, err := store.Screenings().Update(ctx, item)
		return err
	}); err != nil {
		return err
	}

	var request screening.SearchRequest
	if err := json.Unmarshal(item.RequestJSON, &request); err != nil {
		return s.failScreening(ctx, item, err.Error())
	}

	result, err := s.provider.Search(ctx, request)
	if err != nil {
		return s.failScreening(ctx, item, err.Error())
	}

	item.Status = screening.StatusNoHit
	if len(result.Matches) > 0 {
		item.Status = screening.StatusAwaitingReview
	}
	item.ResponseJSON = result.RawResponse
	item.ProviderReference = result.ProviderReference
	item.Partial = result.Partial
	item.UpdatedAt = s.clock.Now()
	completedAt := item.UpdatedAt
	item.CompletedAt = &completedAt
	item.LastError = ""

	matches := make([]screening.Match, 0, len(result.Matches))
	for _, providerMatch := range result.Matches {
		matches = append(matches, screening.Match{
			ID:                           providerMatch.EntityID + "-" + item.ID,
			TenantID:                     item.TenantID,
			ScreeningID:                  item.ID,
			EntityID:                     providerMatch.EntityID,
			Provider:                     item.Provider,
			Status:                       screening.MatchStatusPending,
			Name:                         providerMatch.Name,
			Score:                        providerMatch.Score,
			Payload:                      providerMatch.Payload,
			MatchedTexts:                 providerMatch.MatchedTexts,
			UniqueCounterpartyIdentifier: providerMatch.UniqueCounterpartyIdentifier,
			Enriched:                     false,
			CreatedAt:                    item.UpdatedAt,
			UpdatedAt:                    item.UpdatedAt,
		})
	}

	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.ScreeningMatches().ReplaceForScreening(ctx, item.ID, matches); err != nil {
			return err
		}
		_, err := store.Screenings().Update(ctx, item)
		return err
	})
	if err == nil {
		_ = s.publishScreeningStatusChanged(ctx, item, len(matches))
	}
	return err
}

func (s DispatchService) failScreening(ctx context.Context, item screening.Screening, message string) error {
	now := s.clock.Now()
	item.Status = screening.StatusFailed
	item.LastError = message
	item.UpdatedAt = now
	item.FailedAt = &now
	item.CompletedAt = nil
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		_, err := store.Screenings().Update(ctx, item)
		return err
	})
	if err == nil {
		_ = s.publishScreeningStatusChanged(ctx, item, 0)
	}
	return err
}

func (s DispatchService) publishScreeningStatusChanged(ctx context.Context, item screening.Screening, matchCount int) error {
	if s.decisionPublisher == nil {
		return nil
	}
	return s.decisionPublisher.PublishScreeningStatusChanged(ctx, ports.ScreeningStatusChangedCommand{
		TenantID:          item.TenantID,
		ScreeningID:       item.ID,
		DecisionID:        item.DecisionID,
		ScenarioID:        item.ScenarioID,
		ScreeningConfigID: item.ScreeningConfigID,
		Status:            string(item.Status),
		Provider:          item.Provider,
		ObjectType:        item.ObjectType,
		ObjectID:          item.ObjectID,
		ProviderReference: item.ProviderReference,
		LastError:         item.LastError,
		Partial:           item.Partial,
		IdempotencyKey:    item.IdempotencyKey,
		CompletedAt:       item.CompletedAt,
		MatchCount:        matchCount,
	})
}
