package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type DeferredIngestRepository struct {
	db txExecutor
}

func NewDeferredIngestRepository(db txExecutor) DeferredIngestRepository {
	return DeferredIngestRepository{db: db}
}

func (r DeferredIngestRepository) Create(ctx context.Context, execution ingestion.DeferredIngest) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO core_ingestion.deferred_ingests (
			id, tenant_id, object_type, mode, status, attempt_count, error_message, idempotency_key, payload, requested_at, started_at, completed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, execution.ID, execution.TenantID, execution.ObjectType, string(execution.Mode), string(execution.Status), execution.AttemptCount, execution.ErrorMessage, execution.IdempotencyKey, execution.Payload, execution.RequestedAt, execution.StartedAt, execution.CompletedAt)
	return err
}

func (r DeferredIngestRepository) GetByID(ctx context.Context, id uuid.UUID) (ingestion.DeferredIngest, error) {
	var execution ingestion.DeferredIngest
	err := r.db.QueryRow(ctx, `
		SELECT id, tenant_id, object_type, mode, status, attempt_count, error_message, idempotency_key, payload, requested_at, started_at, completed_at
		FROM core_ingestion.deferred_ingests
		WHERE id = $1
	`, id).Scan(&execution.ID, &execution.TenantID, &execution.ObjectType, &execution.Mode, &execution.Status, &execution.AttemptCount, &execution.ErrorMessage, &execution.IdempotencyKey, &execution.Payload, &execution.RequestedAt, &execution.StartedAt, &execution.CompletedAt)
	return execution, err
}

func (r DeferredIngestRepository) Update(ctx context.Context, execution ingestion.DeferredIngest) error {
	_, err := r.db.Exec(ctx, `
		UPDATE core_ingestion.deferred_ingests
		SET status = $2, attempt_count = $3, error_message = $4, idempotency_key = $5, payload = $6, started_at = $7, completed_at = $8
		WHERE id = $1
	`, execution.ID, string(execution.Status), execution.AttemptCount, execution.ErrorMessage, execution.IdempotencyKey, execution.Payload, execution.StartedAt, execution.CompletedAt)
	return err
}

func (r DeferredIngestRepository) StartAttempt(ctx context.Context, id uuid.UUID, startedAt time.Time) (ingestion.DeferredIngest, error) {
	var execution ingestion.DeferredIngest
	err := r.db.QueryRow(ctx, `
		UPDATE core_ingestion.deferred_ingests
		SET status = 'processing', started_at = $2, attempt_count = attempt_count + 1
		WHERE id = $1
		RETURNING id, tenant_id, object_type, mode, status, attempt_count, error_message, idempotency_key, payload, requested_at, started_at, completed_at
	`, id, startedAt).Scan(&execution.ID, &execution.TenantID, &execution.ObjectType, &execution.Mode, &execution.Status, &execution.AttemptCount, &execution.ErrorMessage, &execution.IdempotencyKey, &execution.Payload, &execution.RequestedAt, &execution.StartedAt, &execution.CompletedAt)
	if err != nil {
		return ingestion.DeferredIngest{}, fmt.Errorf("start deferred ingest attempt: %w", err)
	}
	return execution, nil
}

func (r DeferredIngestRepository) MetricsSnapshot(ctx context.Context, now time.Time) (ingestion.DeferredIngestMetrics, error) {
	var metrics ingestion.DeferredIngestMetrics
	var oldestQueuedRequestedAt *time.Time
	var recentSuccessCount, recentFailureCount, recentRetryCount int

	err := r.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'queued') AS queued_count,
			COUNT(*) FILTER (WHERE status = 'processing') AS processing_count,
			COUNT(*) FILTER (WHERE status = 'completed') AS completed_count,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed_count,
			COUNT(*) FILTER (WHERE status = 'queued' AND attempt_count > 0) AS retry_pending_count,
			COUNT(*) FILTER (WHERE attempt_count > 1) AS retried_execution_count,
			MIN(requested_at) FILTER (WHERE status = 'queued') AS oldest_queued_requested_at,
			COUNT(*) FILTER (WHERE status = 'completed' AND completed_at >= $1 - INTERVAL '5 minutes') AS recent_success_count,
			COUNT(*) FILTER (WHERE status = 'failed' AND completed_at >= $1 - INTERVAL '5 minutes') AS recent_failure_count,
			COUNT(*) FILTER (WHERE attempt_count > 1 AND started_at >= $1 - INTERVAL '5 minutes') AS recent_retry_count
		FROM core_ingestion.deferred_ingests
	`, now).Scan(
		&metrics.QueuedCount,
		&metrics.ProcessingCount,
		&metrics.CompletedCount,
		&metrics.FailedCount,
		&metrics.RetryPendingCount,
		&metrics.RetriedExecutionCount,
		&oldestQueuedRequestedAt,
		&recentSuccessCount,
		&recentFailureCount,
		&recentRetryCount,
	)
	if err != nil {
		return ingestion.DeferredIngestMetrics{}, err
	}

	metrics.OldestQueuedRequestedAt = oldestQueuedRequestedAt
	if oldestQueuedRequestedAt != nil {
		metrics.OldestQueuedAgeSeconds = now.Sub(*oldestQueuedRequestedAt).Seconds()
	}
	metrics.RecentSuccessCount = recentSuccessCount
	metrics.RecentFailureCount = recentFailureCount
	metrics.RecentRetryCount = recentRetryCount
	metrics.DrainRatePerMinuteLast5Min = float64(recentSuccessCount) / 5.0
	metrics.SnapshotAt = now
	return metrics, nil
}
