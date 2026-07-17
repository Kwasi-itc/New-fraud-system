package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type UploadLogRepository struct {
	db txExecutor
}

func NewUploadLogRepository(db txExecutor) UploadLogRepository {
	return UploadLogRepository{db: db}
}

func (r UploadLogRepository) Create(ctx context.Context, log ingestion.UploadLog) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO core_ingestion.upload_logs (
			id, tenant_id, object_type, mode, filename, content_type, status, total_rows, successful_rows, failed_rows, attempt_count, error_message, payload, requested_at, started_at, completed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
	`, log.ID, log.TenantID, log.ObjectType, string(log.Mode), log.Filename, log.ContentType, string(log.Status), log.TotalRows, log.SuccessfulRows, log.FailedRows, log.AttemptCount, log.ErrorMessage, log.Payload, log.RequestedAt, log.StartedAt, log.CompletedAt)
	return err
}

func (r UploadLogRepository) ListByTenantAndObjectType(ctx context.Context, tenantID uuid.UUID, objectType string) ([]ingestion.UploadLog, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, tenant_id, object_type, mode, filename, content_type, status, total_rows, successful_rows, failed_rows, error_message, payload, requested_at, started_at, completed_at
		       , attempt_count
		FROM core_ingestion.upload_logs
		WHERE tenant_id = $1 AND object_type = $2
		ORDER BY requested_at DESC
	`, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]ingestion.UploadLog, 0)
	for rows.Next() {
		var log ingestion.UploadLog
		if err := rows.Scan(&log.ID, &log.TenantID, &log.ObjectType, &log.Mode, &log.Filename, &log.ContentType, &log.Status, &log.TotalRows, &log.SuccessfulRows, &log.FailedRows, &log.ErrorMessage, &log.Payload, &log.RequestedAt, &log.StartedAt, &log.CompletedAt, &log.AttemptCount); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func (r UploadLogRepository) GetByID(ctx context.Context, id uuid.UUID) (ingestion.UploadLog, error) {
	var log ingestion.UploadLog
	err := r.db.QueryRow(ctx, `
		SELECT id, tenant_id, object_type, mode, filename, content_type, status, total_rows, successful_rows, failed_rows, error_message, payload, requested_at, started_at, completed_at, attempt_count
		FROM core_ingestion.upload_logs
		WHERE id = $1
	`, id).Scan(&log.ID, &log.TenantID, &log.ObjectType, &log.Mode, &log.Filename, &log.ContentType, &log.Status, &log.TotalRows, &log.SuccessfulRows, &log.FailedRows, &log.ErrorMessage, &log.Payload, &log.RequestedAt, &log.StartedAt, &log.CompletedAt, &log.AttemptCount)
	return log, err
}

func (r UploadLogRepository) Update(ctx context.Context, log ingestion.UploadLog) error {
	_, err := r.db.Exec(ctx, `
		UPDATE core_ingestion.upload_logs
		SET status = $2, total_rows = $3, successful_rows = $4, failed_rows = $5, attempt_count = $6, error_message = $7, payload = $8, started_at = $9, completed_at = $10
		WHERE id = $1
	`, log.ID, string(log.Status), log.TotalRows, log.SuccessfulRows, log.FailedRows, log.AttemptCount, log.ErrorMessage, log.Payload, log.StartedAt, log.CompletedAt)
	return err
}

func (r UploadLogRepository) StartAttempt(ctx context.Context, id uuid.UUID, startedAt time.Time) (ingestion.UploadLog, error) {
	var log ingestion.UploadLog
	err := r.db.QueryRow(ctx, `
		UPDATE core_ingestion.upload_logs
		SET status = 'processing', started_at = $2, attempt_count = attempt_count + 1
		WHERE id = $1
		RETURNING id, tenant_id, object_type, mode, filename, content_type, status, total_rows, successful_rows, failed_rows, error_message, payload, requested_at, started_at, completed_at, attempt_count
	`, id, startedAt).Scan(&log.ID, &log.TenantID, &log.ObjectType, &log.Mode, &log.Filename, &log.ContentType, &log.Status, &log.TotalRows, &log.SuccessfulRows, &log.FailedRows, &log.ErrorMessage, &log.Payload, &log.RequestedAt, &log.StartedAt, &log.CompletedAt, &log.AttemptCount)
	if err != nil {
		return ingestion.UploadLog{}, fmt.Errorf("start upload log attempt: %w", err)
	}
	return log, nil
}
