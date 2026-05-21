package postgres

import (
	"context"

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
		SELECT tenant_id, key, request_hash, response_kind, response_payload, created_at
		FROM core_ingestion.idempotency_keys
		WHERE tenant_id = $1 AND key = $2
	`, tenantID, key).Scan(&record.TenantID, &record.Key, &record.RequestHash, &record.ResponseKind, &record.ResponsePayload, &record.CreatedAt)
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
		INSERT INTO core_ingestion.idempotency_keys (tenant_id, key, request_hash, response_kind, response_payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, record.TenantID, record.Key, record.RequestHash, record.ResponseKind, record.ResponsePayload, record.CreatedAt)
	return err
}
