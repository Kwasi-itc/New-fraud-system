package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type txExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type IngestionAuditRepository struct {
	db txExecutor
}

func NewIngestionAuditRepository(db txExecutor) IngestionAuditRepository {
	return IngestionAuditRepository{db: db}
}

func (r IngestionAuditRepository) Create(ctx context.Context, audit ingestion.IngestionAudit) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO core_ingestion.ingestion_audit (
			id, tenant_id, object_type, object_id, mode, revision_id, status, payload, validation_errors, idempotency_key, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, audit.ID, audit.TenantID, audit.ObjectType, audit.ObjectID, string(audit.Mode), audit.RevisionID, audit.Status, audit.Payload, audit.ValidationError, audit.IdempotencyKey, audit.CreatedAt)
	return err
}
