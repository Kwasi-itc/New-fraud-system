package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type IndexJobRepository struct {
	db executor
}

func NewIndexJobRepository(db executor) IndexJobRepository {
	return IndexJobRepository{db: db}
}

func (r IndexJobRepository) Create(ctx context.Context, job datamodel.IndexJob) error {
	query := `
		INSERT INTO core.index_jobs (
			id, tenant_id, table_id, table_name, index_type, columns, status,
			requested_by_operation, error_message, attempt_count, requested_at,
			started_at, completed_at, scheduled_at, dedupe_key
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	if _, err := r.db.Exec(ctx, query,
		job.ID, job.TenantID, job.TableID, job.TableName, job.IndexType, job.Columns, job.Status,
		job.RequestedByOperation, job.ErrorMessage, job.AttemptCount, job.RequestedAt,
		job.StartedAt, job.CompletedAt, job.ScheduledAt, job.DedupeKey,
	); err != nil {
		return fmt.Errorf("insert index job: %w", err)
	}
	return nil
}

func (r IndexJobRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error) {
	query := `
		SELECT id, tenant_id, table_id, table_name, index_type, columns, status,
			requested_by_operation, error_message, attempt_count, requested_at,
			started_at, completed_at, scheduled_at, dedupe_key
		FROM core.index_jobs
		WHERE id = $1
	`
	return scanIndexJob(r.db.QueryRow(ctx, query, id))
}

func (r IndexJobRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.IndexJob, error) {
	query := `
		SELECT id, tenant_id, table_id, table_name, index_type, columns, status,
			requested_by_operation, error_message, attempt_count, requested_at,
			started_at, completed_at, scheduled_at, dedupe_key
		FROM core.index_jobs
		WHERE tenant_id = $1
		ORDER BY requested_at DESC, id DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list index jobs: %w", err)
	}
	defer rows.Close()

	var jobs []datamodel.IndexJob
	for rows.Next() {
		job, err := scanIndexJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate index jobs: %w", err)
	}
	return jobs, nil
}

func (r IndexJobRepository) StartAttempt(ctx context.Context, id uuid.UUID, startedAt time.Time) (datamodel.IndexJob, error) {
	query := `
		UPDATE core.index_jobs
		SET status = $2,
			attempt_count = attempt_count + 1,
			started_at = $3,
			error_message = NULL,
			completed_at = NULL
		WHERE id = $1
		RETURNING id, tenant_id, table_id, table_name, index_type, columns, status,
			requested_by_operation, error_message, attempt_count, requested_at,
			started_at, completed_at, scheduled_at, dedupe_key
	`
	job, err := scanIndexJob(r.db.QueryRow(ctx, query, id, datamodel.IndexJobStatusRunning, startedAt))
	if err != nil {
		return datamodel.IndexJob{}, fmt.Errorf("start index job attempt: %w", err)
	}
	return job, nil
}

func (r IndexJobRepository) MarkApplied(ctx context.Context, id uuid.UUID, completedAt time.Time) error {
	query := `
		UPDATE core.index_jobs
		SET status = $2, completed_at = $3, error_message = NULL
		WHERE id = $1
	`
	if _, err := r.db.Exec(ctx, query, id, datamodel.IndexJobStatusApplied, completedAt); err != nil {
		return fmt.Errorf("mark index job applied: %w", err)
	}
	return nil
}

func (r IndexJobRepository) MarkFailed(ctx context.Context, id uuid.UUID, message string, completedAt time.Time) error {
	query := `
		UPDATE core.index_jobs
		SET status = $2, error_message = $3, completed_at = $4
		WHERE id = $1
	`
	if _, err := r.db.Exec(ctx, query, id, datamodel.IndexJobStatusFailed, message, completedAt); err != nil {
		return fmt.Errorf("mark index job failed: %w", err)
	}
	return nil
}

func (r IndexJobRepository) MarkPendingRetry(ctx context.Context, id uuid.UUID, message string) error {
	query := `
		UPDATE core.index_jobs
		SET status = $2,
			error_message = $3,
			completed_at = NULL
		WHERE id = $1
	`
	if _, err := r.db.Exec(ctx, query, id, datamodel.IndexJobStatusPending, message); err != nil {
		return fmt.Errorf("mark index job pending retry: %w", err)
	}
	return nil
}

func (r IndexJobRepository) Retry(ctx context.Context, id uuid.UUID, scheduledAt time.Time) error {
	query := `
		UPDATE core.index_jobs
		SET status = $2, error_message = NULL, started_at = NULL, completed_at = NULL, scheduled_at = $3
		WHERE id = $1
	`
	if _, err := r.db.Exec(ctx, query, id, datamodel.IndexJobStatusPending, scheduledAt); err != nil {
		return fmt.Errorf("retry index job: %w", err)
	}
	return nil
}

type indexJobScanner interface {
	Scan(dest ...any) error
}

func scanIndexJob(scanner indexJobScanner) (datamodel.IndexJob, error) {
	var (
		job          datamodel.IndexJob
		tableID      *uuid.UUID
		errorMessage *string
		startedAt    *time.Time
		completedAt  *time.Time
		scheduledAt  *time.Time
		indexType    string
		status       string
	)
	if err := scanner.Scan(
		&job.ID,
		&job.TenantID,
		&tableID,
		&job.TableName,
		&indexType,
		&job.Columns,
		&status,
		&job.RequestedByOperation,
		&errorMessage,
		&job.AttemptCount,
		&job.RequestedAt,
		&startedAt,
		&completedAt,
		&scheduledAt,
		&job.DedupeKey,
	); err != nil {
		return datamodel.IndexJob{}, err
	}
	job.TableID = tableID
	job.IndexType = datamodel.IndexJobType(indexType)
	job.Status = datamodel.IndexJobStatus(status)
	job.ErrorMessage = errorMessage
	job.StartedAt = startedAt
	job.CompletedAt = completedAt
	job.ScheduledAt = scheduledAt
	return job, nil
}
