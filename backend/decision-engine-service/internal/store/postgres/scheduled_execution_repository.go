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
		with inserted as (
			insert into core.scheduled_executions (
				id, tenant_id, scenario_id, scenario_iteration_id, source, status, idempotency_key, attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			on conflict do nothing
			returning id, tenant_id, scenario_id, scenario_iteration_id, source, status, idempotency_key, attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
		)
		select id, tenant_id, scenario_id, scenario_iteration_id, source, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
		from inserted
		union all
		select id, tenant_id, scenario_id, scenario_iteration_id, source, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
		from core.scheduled_executions
		where tenant_id = $2 and (
			($7 is not null and idempotency_key = $7) or
			($5 = 'recurring' and scenario_id = $3 and scheduled_for = $10 and source = $5)
		)
		limit 1
	`
	var out execution.ScheduledExecution
	var source string
	var status string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.ScenarioIterationID, string(item.Source), string(item.Status), nullableEmptyString(item.IdempotencyKey), item.AttemptCount, item.MaxAttempts, item.ScheduledFor, item.NextAttemptAt, item.RequestBody, item.LastError, item.CreatedAt, item.FailedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &source, &status, &out.IdempotencyKey, &out.AttemptCount, &out.MaxAttempts, &out.ScheduledFor, &out.NextAttemptAt, &out.RequestBody, &out.LastError, &out.CreatedAt, &out.FailedAt)
	out.Source = execution.Source(source)
	out.Status = execution.Status(status)
	return out, err
}

func (r ScheduledExecutionRepository) GetByID(ctx context.Context, tenantID, scenarioID, executionID string) (execution.ScheduledExecution, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, source, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
		from core.scheduled_executions
		where tenant_id = $1 and scenario_id = $2 and id = $3
	`
	var out execution.ScheduledExecution
	var source string
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID, executionID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &source, &status, &out.IdempotencyKey, &out.AttemptCount, &out.MaxAttempts, &out.ScheduledFor, &out.NextAttemptAt, &out.RequestBody, &out.LastError, &out.CreatedAt, &out.FailedAt)
	out.Source = execution.Source(source)
	out.Status = execution.Status(status)
	return out, err
}

func (r ScheduledExecutionRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]execution.ScheduledExecution, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, source, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
		from core.scheduled_executions
		where tenant_id = $1 and scenario_id = $2
		order by scheduled_for desc, created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []execution.ScheduledExecution
	for rows.Next() {
		var item execution.ScheduledExecution
		var source string
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &source, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.ScheduledFor, &item.NextAttemptAt, &item.RequestBody, &item.LastError, &item.CreatedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Source = execution.Source(source)
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScheduledExecutionRepository) CountByStatus(ctx context.Context, tenantID, scenarioID string) (map[execution.Status]int, error) {
	const stmt = `
		select status, count(*)
		from core.scheduled_executions
		where tenant_id = $1 and scenario_id = $2
		group by status
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[execution.Status]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[execution.Status(status)] = count
	}
	return counts, rows.Err()
}

func (r ScheduledExecutionRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]execution.ScheduledExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, source, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
		from core.scheduled_executions
		where status = 'pending' and scheduled_for <= $1 and (next_attempt_at is null or next_attempt_at <= $1)
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
		var source string
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &source, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.ScheduledFor, &item.NextAttemptAt, &item.RequestBody, &item.LastError, &item.CreatedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Source = execution.Source(source)
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScheduledExecutionRepository) StartAttempt(ctx context.Context, id string) (execution.ScheduledExecution, error) {
	const stmt = `
		update core.scheduled_executions
		set status = 'running',
		    attempt_count = attempt_count + 1,
		    next_attempt_at = null
		where id = $1 and status = 'pending'
		returning id, tenant_id, scenario_id, scenario_iteration_id, source, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, scheduled_for, next_attempt_at, request_body, last_error, created_at, failed_at
	`
	var item execution.ScheduledExecution
	var source string
	var status string
	err := r.q.QueryRow(ctx, stmt, id).
		Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &source, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.ScheduledFor, &item.NextAttemptAt, &item.RequestBody, &item.LastError, &item.CreatedAt, &item.FailedAt)
	item.Source = execution.Source(source)
	item.Status = execution.Status(status)
	return item, err
}

func (r ScheduledExecutionRepository) UpdateStatus(ctx context.Context, id string, status execution.Status) error {
	const stmt = `update core.scheduled_executions set status = $1, next_attempt_at = null, failed_at = case when $1 = 'failed' then now() else failed_at end where id = $2`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}

func (r ScheduledExecutionRepository) RecordAttemptFailure(ctx context.Context, id string, status execution.Status, nextAttemptAt *time.Time, lastError string, failedAt *time.Time) error {
	const stmt = `
		update core.scheduled_executions
		set status = $1,
		    next_attempt_at = $2,
		    last_error = $3,
		    failed_at = $4
		where id = $5
	`
	_, err := r.q.Exec(ctx, stmt, string(status), nextAttemptAt, lastError, failedAt, id)
	return err
}

func (r ScheduledExecutionRepository) ResetForRetry(ctx context.Context, id string, status execution.Status) error {
	const stmt = `
		update core.scheduled_executions
		set status = $1,
		    attempt_count = 0,
		    next_attempt_at = null,
		    last_error = '',
		    failed_at = null
		where id = $2
	`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}
