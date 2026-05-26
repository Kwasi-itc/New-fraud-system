package postgres

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
)

type ScheduledExecutionRepository struct{ q queryable }

func NewScheduledExecutionRepository(q queryable) ScheduledExecutionRepository {
	return ScheduledExecutionRepository{q: q}
}

func (r ScheduledExecutionRepository) Create(ctx context.Context, item execution.ScheduledExecution) (execution.ScheduledExecution, error) {
	const stmt = `
		insert into core.scheduled_executions (
			id, tenant_id, scenario_id, scenario_iteration_id, status, scheduled_for, request_body, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8)
		returning id, tenant_id, scenario_id, scenario_iteration_id, status, scheduled_for, request_body, created_at
	`
	var out execution.ScheduledExecution
	var status string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.ScenarioIterationID, string(item.Status), item.ScheduledFor, item.RequestBody, item.CreatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &status, &out.ScheduledFor, &out.RequestBody, &out.CreatedAt)
	out.Status = execution.Status(status)
	return out, err
}

func (r ScheduledExecutionRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]execution.ScheduledExecution, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, status, scheduled_for, request_body, created_at
		from core.scheduled_executions
		where tenant_id = $1 and scenario_id = $2
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []execution.ScheduledExecution
	for rows.Next() {
		var item execution.ScheduledExecution
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &status, &item.ScheduledFor, &item.RequestBody, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScheduledExecutionRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]execution.ScheduledExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, status, scheduled_for, request_body, created_at
		from core.scheduled_executions
		where status = 'pending' and scheduled_for <= $1
		order by scheduled_for asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []execution.ScheduledExecution
	for rows.Next() {
		var item execution.ScheduledExecution
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &status, &item.ScheduledFor, &item.RequestBody, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScheduledExecutionRepository) UpdateStatus(ctx context.Context, id string, status execution.Status) error {
	const stmt = `update core.scheduled_executions set status = $1 where id = $2`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}
