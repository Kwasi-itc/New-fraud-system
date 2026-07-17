package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
)

type OutboxEventRepository struct{ q queryable }

func NewOutboxEventRepository(q queryable) OutboxEventRepository { return OutboxEventRepository{q: q} }

func (r OutboxEventRepository) CreateMany(ctx context.Context, items []integration.OutboxEvent) ([]integration.OutboxEvent, error) {
	if len(items) == 0 {
		return []integration.OutboxEvent{}, nil
	}
	const stmt = `
		insert into core.outbox_events (
			id, tenant_id, aggregate_type, aggregate_id, event_type, payload, status, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8)
		returning id, tenant_id, aggregate_type, aggregate_id, event_type, payload, status, created_at
	`
	out := make([]integration.OutboxEvent, 0, len(items))
	for _, item := range items {
		var stored integration.OutboxEvent
		var status string
		err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.AggregateType, item.AggregateID, item.EventType, item.Payload, string(item.Status), item.CreatedAt).
			Scan(&stored.ID, &stored.TenantID, &stored.AggregateType, &stored.AggregateID, &stored.EventType, &stored.Payload, &status, &stored.CreatedAt)
		if err != nil {
			return nil, err
		}
		stored.Status = integration.OutboxStatus(status)
		out = append(out, stored)
	}
	return out, nil
}

func (r OutboxEventRepository) GetByID(ctx context.Context, tenantID, eventID string) (integration.OutboxEvent, error) {
	const stmt = `
		select id, tenant_id, aggregate_type, aggregate_id, event_type, payload, status, created_at
		from core.outbox_events
		where tenant_id = $1 and id = $2
	`
	var item integration.OutboxEvent
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, eventID).
		Scan(&item.ID, &item.TenantID, &item.AggregateType, &item.AggregateID, &item.EventType, &item.Payload, &status, &item.CreatedAt)
	item.Status = integration.OutboxStatus(status)
	return item, err
}

func (r OutboxEventRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]integration.OutboxEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, aggregate_type, aggregate_id, event_type, payload, status, created_at
		from core.outbox_events
		where tenant_id = $1
		order by created_at desc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []integration.OutboxEvent
	for rows.Next() {
		var item integration.OutboxEvent
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.AggregateType, &item.AggregateID, &item.EventType, &item.Payload, &status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Status = integration.OutboxStatus(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r OutboxEventRepository) ListByStatus(ctx context.Context, status integration.OutboxStatus, limit int) ([]integration.OutboxEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, aggregate_type, aggregate_id, event_type, payload, status, created_at
		from core.outbox_events
		where status = $1
		order by created_at asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []integration.OutboxEvent
	for rows.Next() {
		var item integration.OutboxEvent
		var statusValue string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.AggregateType, &item.AggregateID, &item.EventType, &item.Payload, &statusValue, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Status = integration.OutboxStatus(statusValue)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r OutboxEventRepository) UpdateStatus(ctx context.Context, id string, status integration.OutboxStatus) error {
	const stmt = `update core.outbox_events set status = $1 where id = $2`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}
