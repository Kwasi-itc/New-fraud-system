package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
)

type AsyncDecisionExecutionRepository struct{ q queryable }

func NewAsyncDecisionExecutionRepository(q queryable) AsyncDecisionExecutionRepository {
	return AsyncDecisionExecutionRepository{q: q}
}

func (r AsyncDecisionExecutionRepository) Create(ctx context.Context, item execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error) {
	const stmt = `
		insert into core.async_decision_executions (
			id, tenant_id, scenario_id, object_type, status, request_body, created_at
		) values ($1,$2,$3,$4,$5,$6,$7)
		returning id, tenant_id, scenario_id, object_type, status, request_body, created_at
	`
	var out execution.AsyncDecisionExecution
	var status string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.ObjectType, string(item.Status), item.RequestBody, item.CreatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ObjectType, &status, &out.RequestBody, &out.CreatedAt)
	out.Status = execution.Status(status)
	return out, err
}

func (r AsyncDecisionExecutionRepository) ListByTenant(ctx context.Context, tenantID string) ([]execution.AsyncDecisionExecution, error) {
	const stmt = `
		select id, tenant_id, scenario_id, object_type, status, request_body, created_at
		from core.async_decision_executions
		where tenant_id = $1
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []execution.AsyncDecisionExecution
	for rows.Next() {
		var item execution.AsyncDecisionExecution
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.RequestBody, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r AsyncDecisionExecutionRepository) ListQueued(ctx context.Context, limit int) ([]execution.AsyncDecisionExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, scenario_id, object_type, status, request_body, created_at
		from core.async_decision_executions
		where status = 'queued'
		order by created_at asc
		limit $1
	`
	rows, err := r.q.Query(ctx, stmt, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []execution.AsyncDecisionExecution
	for rows.Next() {
		var item execution.AsyncDecisionExecution
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.RequestBody, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r AsyncDecisionExecutionRepository) UpdateStatus(ctx context.Context, id string, status execution.Status) error {
	const stmt = `update core.async_decision_executions set status = $1 where id = $2`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}
