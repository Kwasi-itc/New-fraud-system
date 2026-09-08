package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type IdempotencyRepository struct {
	db txExecutor
}

func NewIdempotencyRepository(db txExecutor) IdempotencyRepository {
	return IdempotencyRepository{db: db}
}

func (r IdempotencyRepository) Get(ctx context.Context, tenantID uuid.UUID, key string) (*ingestion.IdempotencyKey, error) {
	var record ingestion.IdempotencyKey
	err := r.db.QueryRow(ctx, `
		SELECT tenant_id, key, request_hash, response_kind, response_payload,
		       fact_marker, fact_manifest, fact_status, fact_updated_at, created_at
		FROM core_ingestion.idempotency_keys
		WHERE tenant_id = $1 AND key = $2
	`, tenantID, key).Scan(
		&record.TenantID, &record.Key, &record.RequestHash, &record.ResponseKind, &record.ResponsePayload,
		&record.FactMarker, &record.FactManifest, &record.FactStatus, &record.FactUpdatedAt, &record.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (r IdempotencyRepository) Create(ctx context.Context, record ingestion.IdempotencyKey) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO core_ingestion.idempotency_keys (
			tenant_id, key, request_hash, response_kind, response_payload,
			fact_marker, fact_manifest, fact_status, fact_updated_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, record.TenantID, record.Key, record.RequestHash, record.ResponseKind, record.ResponsePayload,
		record.FactMarker, record.FactManifest, record.FactStatus, record.FactUpdatedAt, record.CreatedAt)
	return err
}

func (r IdempotencyRepository) MarkFactApplied(ctx context.Context, tenantID uuid.UUID, key, marker, manifest string) error {
	result, err := r.db.Exec(ctx, `
		UPDATE core_ingestion.idempotency_keys
		SET fact_status = NULL, fact_marker = NULL, fact_manifest = NULL, fact_updated_at = NULL
		WHERE tenant_id = $1 AND key = $2
		  AND fact_marker = $3 AND fact_manifest = $4
		  AND fact_status = 'pending'
	`, tenantID, key, marker, manifest)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var status *string
	if err := r.db.QueryRow(ctx, `
		SELECT fact_status
		FROM core_ingestion.idempotency_keys
		WHERE tenant_id = $1 AND key = $2
	`, tenantID, key).Scan(&status); err != nil {
		return fmt.Errorf("verify aggregate fact journal completion: %w", err)
	}
	if status == nil || *status == "applied" {
		return nil
	}
	return fmt.Errorf("aggregate fact journal does not match idempotency record")
}
