package postgres

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
)

type AsyncDecisionExecutionRepository struct{ q queryable }

func NewAsyncDecisionExecutionRepository(q queryable) AsyncDecisionExecutionRepository {
	return AsyncDecisionExecutionRepository{q: q}
}

func (r AsyncDecisionExecutionRepository) Create(ctx context.Context, item execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error) {
	const stmt = `
		with inserted as (
			insert into core.async_decision_executions (
				id, tenant_id, scenario_id, object_type, status, idempotency_key, attempt_count, max_attempts, next_attempt_at, request_body, last_error, created_at, failed_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			on conflict do nothing
			returning id, tenant_id, scenario_id, object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at, request_body, last_error, created_at, failed_at
		)
		select id, tenant_id, scenario_id, object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at, request_body, last_error, created_at, failed_at
		from inserted
		union all
		select id, tenant_id, scenario_id, object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at, request_body, last_error, created_at, failed_at
		from core.async_decision_executions
		where tenant_id = $2 and $6 is not null and idempotency_key = $6
		limit 1
	`
	var out execution.AsyncDecisionExecution
	var status string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.ObjectType, string(item.Status), nullableEmptyString(item.IdempotencyKey), item.AttemptCount, item.MaxAttempts, item.NextAttemptAt, item.RequestBody, item.LastError, item.CreatedAt, item.FailedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ObjectType, &status, &out.IdempotencyKey, &out.AttemptCount, &out.MaxAttempts, &out.NextAttemptAt, &out.RequestBody, &out.LastError, &out.CreatedAt, &out.FailedAt)
	out.Status = execution.Status(status)
	return out, err
}

func (r AsyncDecisionExecutionRepository) GetByID(ctx context.Context, tenantID, executionID string) (execution.AsyncDecisionExecution, error) {
	const stmt = `
		select id, tenant_id, scenario_id, object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at, request_body, last_error, created_at, failed_at
		from core.async_decision_executions
		where tenant_id = $1 and id = $2
	`
	var item execution.AsyncDecisionExecution
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, executionID).
		Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt, &item.RequestBody, &item.LastError, &item.CreatedAt, &item.FailedAt)
	item.Status = execution.Status(status)
	return item, err
}

func (r AsyncDecisionExecutionRepository) ListByTenant(ctx context.Context, tenantID string) ([]execution.AsyncDecisionExecution, error) {
	const stmt = `
		select id, tenant_id, scenario_id, object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at, request_body, last_error, created_at, failed_at
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
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt, &item.RequestBody, &item.LastError, &item.CreatedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r AsyncDecisionExecutionRepository) CountByStatus(ctx context.Context, tenantID string) (map[execution.Status]int, error) {
	const stmt = `
		select status, count(*)
		from core.async_decision_executions
		where tenant_id = $1
		group by status
	`
	rows, err := r.q.Query(ctx, stmt, tenantID)
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

func (r AsyncDecisionExecutionRepository) ListQueued(ctx context.Context, limit int) ([]execution.AsyncDecisionExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, scenario_id, object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at, request_body, last_error, created_at, failed_at
		from core.async_decision_executions
		where status = 'queued' and (next_attempt_at is null or next_attempt_at <= now())
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
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt, &item.RequestBody, &item.LastError, &item.CreatedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r AsyncDecisionExecutionRepository) ClaimQueued(ctx context.Context, limit int) ([]execution.AsyncDecisionExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		with queued as (
			select id
			from core.async_decision_executions
			where status = 'queued' and (next_attempt_at is null or next_attempt_at <= now())
			order by created_at asc
			limit $1
			for update skip locked
		)
		update core.async_decision_executions ade
		set status = 'running',
		    attempt_count = ade.attempt_count + 1,
		    next_attempt_at = null
		from queued
		where ade.id = queued.id
		returning ade.id, ade.tenant_id, ade.scenario_id, ade.object_type, ade.status, coalesce(ade.idempotency_key, ''), ade.attempt_count, ade.max_attempts, ade.next_attempt_at, ade.request_body, ade.last_error, ade.created_at, ade.failed_at
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
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt, &item.RequestBody, &item.LastError, &item.CreatedAt, &item.FailedAt); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r AsyncDecisionExecutionRepository) UpdateStatus(ctx context.Context, id string, status execution.Status) error {
	const stmt = `update core.async_decision_executions set status = $1, next_attempt_at = null, failed_at = case when $1 = 'failed' then now() else failed_at end where id = $2`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}

func (r AsyncDecisionExecutionRepository) RecordAttemptFailure(ctx context.Context, id string, status execution.Status, nextAttemptAt *time.Time, lastError string, failedAt *time.Time) error {
	const stmt = `
		update core.async_decision_executions
		set status = $1,
		    next_attempt_at = $2,
		    last_error = $3,
		    failed_at = $4
		where id = $5
	`
	_, err := r.q.Exec(ctx, stmt, string(status), nextAttemptAt, lastError, failedAt, id)
	return err
}

func (r AsyncDecisionExecutionRepository) ResetForRetry(ctx context.Context, id string, status execution.Status) error {
	const stmt = `
		update core.async_decision_executions
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
