package mysql

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type OutboxEventRepository struct{ authoringRepository }

var _ ports.OutboxEventRepository = OutboxEventRepository{}

func (r OutboxEventRepository) selectSQL() string {
	return "SELECT id,tenant_id,status,created_at," + r.project([]string{"aggregate_type", "aggregate_id", "event_type"}, "payload") + " FROM " + r.schema + ".decision_engine_outbox_events"
}
func (r OutboxEventRepository) scan(row rowScanner) (integration.OutboxEvent, int64, error) {
	var id, t mysqlstore.UUID
	var status string
	var at mysqlstore.DateTime
	var size int64
	var kind, aggregate, event mysqlstore.Text
	var payload mysqlstore.JSON
	if err := row.Scan(&id, &t, &status, &at, &size, &kind, &aggregate, &event, &payload); err != nil {
		return integration.OutboxEvent{}, 0, err
	}
	return integration.OutboxEvent{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(t).String(), AggregateType: string(kind), AggregateID: string(aggregate), EventType: string(event), Payload: json.RawMessage(payload), Status: integration.OutboxStatus(status), CreatedAt: at.Time}, size, nil
}
func validOutboxStatus(s integration.OutboxStatus) bool {
	switch s {
	case integration.OutboxStatusPending, integration.OutboxStatusSent, integration.OutboxStatusFailed:
		return true
	}
	return false
}
func (r OutboxEventRepository) CreateMany(ctx context.Context, items []integration.OutboxEvent) ([]integration.OutboxEvent, error) {
	if err := r.mutating(); err != nil {
		return nil, err
	}
	if len(items) > r.maxRows {
		return nil, ErrProjectionLimit
	}
	values := make([][]any, len(items))
	ids := make([]string, len(items))
	used := 0
	for i, v := range items {
		id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.AggregateType, v.AggregateID, v.EventType)
		if err != nil {
			return nil, err
		}
		if !validOutboxStatus(v.Status) {
			return nil, errors.New("invalid outbox status")
		}
		payload := mysqlstore.JSON(v.Payload)
		if _, err := payload.Value(); err != nil {
			return nil, err
		}
		used += len(v.AggregateType) + len(v.AggregateID) + len(v.EventType) + len(v.Payload)
		if used > r.maxBytes {
			return nil, ErrProjectionLimit
		}
		ids[i] = v.ID
		values[i] = []any{id, mysqlstore.UUID(r.tenant), mysqlstore.Text(v.AggregateType), mysqlstore.Text(v.AggregateID), mysqlstore.Text(v.EventType), payload, string(v.Status), mysqlstore.DateTime{Time: v.CreatedAt}}
	}
	return insertEffectBatch(ctx, r.authoringRepository, "decision_engine_outbox_events", "id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at", r.selectSQL(), values, ids, r.scan, func(v integration.OutboxEvent) string { return v.ID })
}
func (r OutboxEventRepository) GetByID(ctx context.Context, tenant, id string) (integration.OutboxEvent, error) {
	args, err := r.ids(tenant, id)
	if err != nil {
		return integration.OutboxEvent{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", args...), r.scan)
}
func (r OutboxEventRepository) ListByTenant(ctx context.Context, tenant string, limit int) ([]integration.OutboxEvent, error) {
	if err := r.scope(tenant); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > r.maxRows {
		return nil, ErrProjectionLimit
	}
	rows, err := r.tx.QueryContext(ctx, r.selectSQL()+" WHERE tenant_id=? ORDER BY created_at DESC,id DESC LIMIT ?", mysqlstore.UUID(r.tenant), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]integration.OutboxEvent, 0, limit)
	var used int64
	for rows.Next() {
		v, size, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		if size < 0 || size > int64(r.maxBytes)-used {
			return nil, ErrProjectionLimit
		}
		used += size
		result = append(result, v)
	}
	return result, rows.Err()
}
func (r OutboxEventRepository) ListByStatus(context.Context, integration.OutboxStatus, int) ([]integration.OutboxEvent, error) {
	return nil, ErrSystemScopeRequired
}
func (r OutboxEventRepository) UpdateStatus(ctx context.Context, id string, status integration.OutboxStatus) error {
	if err := r.mutating(); err != nil {
		return err
	}
	if !validOutboxStatus(status) {
		return errors.New("invalid outbox status")
	}
	args, err := r.ids(r.tenant.String(), id)
	if err != nil {
		return err
	}
	var locked mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_outbox_events WHERE tenant_id=? AND id=? FOR UPDATE", args...).Scan(&locked); err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_outbox_events SET status=? WHERE tenant_id=? AND id=?", string(status), args[0], args[1])
	return err
}
