package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type OutboxEventRepository struct {
	db txExecutor
}

func NewOutboxEventRepository(db txExecutor) OutboxEventRepository {
	return OutboxEventRepository{db: db}
}

func (r OutboxEventRepository) Create(ctx context.Context, event ingestion.OutboxEvent) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO core_ingestion.outbox_events (
			id, tenant_id, event_type, aggregate_type, aggregate_key, payload, status, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, event.ID, event.TenantID, event.EventType, event.AggregateType, event.AggregateKey, event.Payload, event.Status, event.CreatedAt)
	return err
}
