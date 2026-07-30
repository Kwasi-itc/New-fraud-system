package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type LogicalBucketRepository struct {
	db executor
}

func NewLogicalBucketRepository(db executor) LogicalBucketRepository {
	return LogicalBucketRepository{db: db}
}

func (r LogicalBucketRepository) Create(ctx context.Context, item datamodel.LogicalBucketDefinition) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO core.logical_bucket_definitions (
			id, tenant_id, table_id, timestamp_field_id, timestamp_field_name,
			grain, timezone, seal_delay_seconds, definition_version, status,
			index_job_id, cache_eligible_at, maintenance_until, created_at,
			updated_at, retired_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
	`, item.ID, item.TenantID, item.TableID, item.TimestampFieldID,
		item.TimestampFieldName, item.Grain, item.Timezone, int64(item.SealDelay/time.Second),
		item.DefinitionVersion, item.Status, item.IndexJobID, item.CacheEligibleAt,
		item.MaintenanceUntil, item.CreatedAt, item.UpdatedAt, item.RetiredAt)
	if err != nil {
		return fmt.Errorf("insert logical bucket definition: %w", err)
	}
	return nil
}

func (r LogicalBucketRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.LogicalBucketDefinition, error) {
	return scanLogicalBucket(r.db.QueryRow(ctx, logicalBucketSelect+` WHERE id = $1`, id))
}

func (r LogicalBucketRepository) GetByIndexJobID(ctx context.Context, jobID uuid.UUID) (datamodel.LogicalBucketDefinition, error) {
	return scanLogicalBucket(r.db.QueryRow(ctx, logicalBucketSelect+` WHERE index_job_id = $1`, jobID))
}

func (r LogicalBucketRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.LogicalBucketDefinition, error) {
	return r.list(ctx, logicalBucketSelect+` WHERE tenant_id = $1 ORDER BY table_id, created_at, id`, tenantID)
}

func (r LogicalBucketRepository) ListByTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.LogicalBucketDefinition, error) {
	return r.list(ctx, logicalBucketSelect+` WHERE table_id = $1 ORDER BY created_at, id`, tableID)
}

func (r LogicalBucketRepository) list(ctx context.Context, query string, arg any) ([]datamodel.LogicalBucketDefinition, error) {
	rows, err := r.db.Query(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("list logical bucket definitions: %w", err)
	}
	defer rows.Close()
	var items []datamodel.LogicalBucketDefinition
	for rows.Next() {
		item, err := scanLogicalBucket(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r LogicalBucketRepository) LockTable(ctx context.Context, tableID uuid.UUID) error {
	var id uuid.UUID
	if err := r.db.QueryRow(ctx, `SELECT id FROM core.model_tables WHERE id = $1 FOR UPDATE`, tableID).Scan(&id); err != nil {
		return fmt.Errorf("lock logical bucket table: %w", err)
	}
	return nil
}

func (r LogicalBucketRepository) AttachIndexJob(ctx context.Context, id, jobID uuid.UUID, updatedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE core.logical_bucket_definitions
		SET index_job_id = $2, updated_at = $3
		WHERE id = $1
	`, id, jobID, updatedAt)
	return err
}

func (r LogicalBucketRepository) MarkActivating(ctx context.Context, id uuid.UUID, eligibleAt, updatedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE core.logical_bucket_definitions
		SET status = 'activating', cache_eligible_at = $2, updated_at = $3
		WHERE id = $1
	`, id, eligibleAt, updatedAt)
	return err
}

func (r LogicalBucketRepository) MarkBlockedData(ctx context.Context, id uuid.UUID, updatedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE core.logical_bucket_definitions
		SET status = 'blocked_data', cache_eligible_at = NULL, updated_at = $2
		WHERE id = $1
	`, id, updatedAt)
	return err
}

func (r LogicalBucketRepository) MarkRetiring(ctx context.Context, id uuid.UUID, maintenanceUntil, updatedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE core.logical_bucket_definitions
		SET status = 'retiring', maintenance_until = $2, updated_at = $3
		WHERE id = $1
	`, id, maintenanceUntil, updatedAt)
	return err
}

func (r LogicalBucketRepository) PromoteLifecycle(ctx context.Context, now time.Time) error {
	if _, err := r.db.Exec(ctx, `
		UPDATE core.logical_bucket_definitions
		SET status = 'active', updated_at = $1
		WHERE status = 'activating' AND cache_eligible_at <= $1
	`, now); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `
		UPDATE core.logical_bucket_definitions
		SET status = 'retired', retired_at = $1, updated_at = $1
		WHERE status = 'retiring' AND maintenance_until <= $1
	`, now); err != nil {
		return err
	}
	return nil
}

const logicalBucketSelect = `
	SELECT id, tenant_id, table_id, timestamp_field_id, timestamp_field_name,
		grain, timezone, seal_delay_seconds, definition_version, status,
		index_job_id, cache_eligible_at, maintenance_until, created_at,
		updated_at, retired_at
	FROM core.logical_bucket_definitions
`

type logicalBucketScanner interface {
	Scan(dest ...any) error
}

func scanLogicalBucket(scanner logicalBucketScanner) (datamodel.LogicalBucketDefinition, error) {
	var item datamodel.LogicalBucketDefinition
	var sealDelaySeconds int64
	var status string
	if err := scanner.Scan(
		&item.ID, &item.TenantID, &item.TableID, &item.TimestampFieldID,
		&item.TimestampFieldName, &item.Grain, &item.Timezone, &sealDelaySeconds,
		&item.DefinitionVersion, &status, &item.IndexJobID, &item.CacheEligibleAt,
		&item.MaintenanceUntil, &item.CreatedAt, &item.UpdatedAt, &item.RetiredAt,
	); err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	item.SealDelay = time.Duration(sealDelaySeconds) * time.Second
	item.Status = datamodel.LogicalBucketStatus(status)
	return item, nil
}
