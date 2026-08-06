package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
)

type DeferredIngestService struct {
	deferredIngests ports.DeferredIngestRepository
	ingestService   IngestService
	txManager       ports.TransactionManager
	idGenerator     ports.IDGenerator
	clock           ports.Clock
	maxAttempts     int
	enqueuer        riverjobs.DeferredIngestEnqueuer
}

func NewDeferredIngestService(
	deferredIngests ports.DeferredIngestRepository,
	ingestService IngestService,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
	maxAttempts int,
	enqueuer riverjobs.DeferredIngestEnqueuer,
) DeferredIngestService {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if enqueuer == nil {
		enqueuer = riverjobs.NoopDeferredIngestEnqueuer{}
	}
	return DeferredIngestService{
		deferredIngests: deferredIngests,
		ingestService:   ingestService,
		txManager:       txManager,
		idGenerator:     idGenerator,
		clock:           clock,
		maxAttempts:     maxAttempts,
		enqueuer:        enqueuer,
	}
}

func (s DeferredIngestService) Create(ctx context.Context, tenantID uuid.UUID, objectType string, mode ingestion.Mode, payload map[string]any, idempotencyKey *string) (ingestion.DeferredIngest, error) {
	now := s.clock.Now()
	execution := ingestion.DeferredIngest{
		ID:             s.idGenerator.New().String(),
		TenantID:       tenantID.String(),
		ObjectType:     objectType,
		Mode:           mode,
		Status:         ingestion.DeferredIngestStatusQueued,
		IdempotencyKey: idempotencyKey,
		Payload:        ingestion.MarshalPayload(payload),
		RequestedAt:    now,
	}
	executionID, err := uuid.Parse(execution.ID)
	if err != nil {
		return ingestion.DeferredIngest{}, err
	}
	if s.txManager == nil {
		if err := s.deferredIngests.Create(ctx, execution); err != nil {
			return ingestion.DeferredIngest{}, err
		}
		if err := s.enqueuer.Enqueue(ctx, executionID, nil); err != nil {
			return ingestion.DeferredIngest{}, err
		}
		return execution, nil
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.DeferredIngests().Create(ctx, execution); err != nil {
			return err
		}
		return s.enqueuer.EnqueueTx(ctx, store.RawTx(), executionID, nil)
	}); err != nil {
		return ingestion.DeferredIngest{}, err
	}
	return execution, nil
}

func (s DeferredIngestService) Get(ctx context.Context, id uuid.UUID) (ingestion.DeferredIngest, error) {
	if s.deferredIngests == nil {
		return ingestion.DeferredIngest{}, fmt.Errorf("deferred ingest repository unavailable")
	}
	return s.deferredIngests.GetByID(ctx, id)
}

func (s DeferredIngestService) MetricsSnapshot(ctx context.Context) (ingestion.DeferredIngestMetrics, error) {
	if s.deferredIngests == nil {
		return ingestion.DeferredIngestMetrics{
			SnapshotAt: s.clock.Now(),
		}, nil
	}
	return s.deferredIngests.MetricsSnapshot(ctx, s.clock.Now())
}

func (s DeferredIngestService) RunDeferredIngest(ctx context.Context, id uuid.UUID) error {
	now := s.clock.Now()
	execution, err := s.deferredIngests.StartAttempt(ctx, id, now)
	if err != nil {
		return err
	}

	var payload map[string]any
	if err := json.Unmarshal(execution.Payload, &payload); err != nil {
		return s.handleRetryableFailure(ctx, &execution, now, fmt.Sprintf("decode deferred ingest payload: %v", err))
	}

	result, validationErrors, err := s.ingestService.Ingest(ctx, IngestInput{
		TenantID:       uuid.MustParse(execution.TenantID),
		ObjectType:     execution.ObjectType,
		Mode:           execution.Mode,
		Payload:        payload,
		IdempotencyKey: execution.IdempotencyKey,
	})
	if err != nil {
		return s.handleRetryableFailure(ctx, &execution, now, err.Error())
	}
	if len(validationErrors) > 0 {
		message := summarizeValidationErrors(validationErrors)
		execution.Status = ingestion.DeferredIngestStatusFailed
		execution.ErrorMessage = &message
		execution.CompletedAt = &now
		return s.deferredIngests.Update(ctx, execution)
	}

	_ = result
	execution.Status = ingestion.DeferredIngestStatusCompleted
	execution.ErrorMessage = nil
	execution.CompletedAt = &now
	return s.deferredIngests.Update(ctx, execution)
}

func (s DeferredIngestService) handleRetryableFailure(ctx context.Context, execution *ingestion.DeferredIngest, now time.Time, message string) error {
	execution.ErrorMessage = &message
	if execution.AttemptCount < s.maxAttempts {
		execution.Status = ingestion.DeferredIngestStatusQueued
		execution.StartedAt = nil
		execution.CompletedAt = nil
		id, err := uuid.Parse(execution.ID)
		if err != nil {
			return err
		}
		if s.txManager == nil {
			if err := s.deferredIngests.Update(ctx, *execution); err != nil {
				return err
			}
			return s.enqueuer.Enqueue(ctx, id, nil)
		}
		return s.txManager.Run(ctx, func(store ports.MutationStore) error {
			if err := store.DeferredIngests().Update(ctx, *execution); err != nil {
				return err
			}
			return s.enqueuer.EnqueueTx(ctx, store.RawTx(), id, nil)
		})
	}
	execution.Status = ingestion.DeferredIngestStatusFailed
	execution.CompletedAt = &now
	return s.deferredIngests.Update(ctx, *execution)
}
