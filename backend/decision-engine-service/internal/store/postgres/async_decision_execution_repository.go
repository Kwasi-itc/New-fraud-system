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
				id, tenant_id, scenario_id, object_type, status, idempotency_key, attempt_count, max_attempts, next_attempt_at,
				request_body, result_body, callback_url, callback_status, callback_attempt_count, callback_last_error,
				callback_sent_at, created_at, completed_at, failed_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
			on conflict do nothing
			returning id, tenant_id, scenario_id::text as scenario_id, object_type, status, idempotency_key, attempt_count, max_attempts, next_attempt_at,
				request_body, result_body, callback_url, callback_status, callback_attempt_count,
				callback_last_error, callback_sent_at, created_at, completed_at, failed_at
		)
		select id, tenant_id, coalesce(scenario_id, ''), object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at,
			request_body, result_body, coalesce(callback_url, ''), coalesce(callback_status, ''), callback_attempt_count,
			coalesce(callback_last_error, ''), callback_sent_at, created_at, completed_at, failed_at
		from inserted
		union all
		select id, tenant_id, coalesce(scenario_id::text, ''), object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at,
			request_body, result_body, coalesce(callback_url, ''), coalesce(callback_status, ''), callback_attempt_count,
			coalesce(callback_last_error, ''), callback_sent_at, created_at, completed_at, failed_at
		from core.async_decision_executions
		where tenant_id = $2 and $6 is not null and idempotency_key = $6
		limit 1
	`
	var out execution.AsyncDecisionExecution
	var status string
	err := r.q.QueryRow(ctx, stmt,
		item.ID, item.TenantID, nullableEmptyString(item.ScenarioID), item.ObjectType, string(item.Status), nullableEmptyString(item.IdempotencyKey),
		item.AttemptCount, item.MaxAttempts, item.NextAttemptAt, item.RequestBody, item.ResultBody,
		nullableEmptyString(item.CallbackURL), nullableEmptyString(item.CallbackStatus), item.CallbackAttemptCount,
		item.CallbackLastError, item.CallbackSentAt, item.CreatedAt, item.CompletedAt, item.FailedAt,
	).Scan(
		&out.ID, &out.TenantID, &out.ScenarioID, &out.ObjectType, &status, &out.IdempotencyKey, &out.AttemptCount, &out.MaxAttempts, &out.NextAttemptAt,
		&out.RequestBody, &out.ResultBody, &out.CallbackURL, &out.CallbackStatus, &out.CallbackAttemptCount,
		&out.CallbackLastError, &out.CallbackSentAt, &out.CreatedAt, &out.CompletedAt, &out.FailedAt,
	)
	out.Status = execution.Status(status)
	return out, err
}

func (r AsyncDecisionExecutionRepository) GetByID(ctx context.Context, tenantID, executionID string) (execution.AsyncDecisionExecution, error) {
	const stmt = `
		select id, tenant_id, coalesce(scenario_id::text, ''), object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at,
			request_body, result_body, coalesce(callback_url, ''), coalesce(callback_status, ''), callback_attempt_count,
			coalesce(callback_last_error, ''), callback_sent_at, created_at, completed_at, failed_at
		from core.async_decision_executions
		where tenant_id = $1 and id = $2
	`
	var item execution.AsyncDecisionExecution
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, executionID).
		Scan(
			&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt,
			&item.RequestBody, &item.ResultBody, &item.CallbackURL, &item.CallbackStatus, &item.CallbackAttemptCount,
			&item.CallbackLastError, &item.CallbackSentAt, &item.CreatedAt, &item.CompletedAt, &item.FailedAt,
		)
	item.Status = execution.Status(status)
	return item, err
}

func (r AsyncDecisionExecutionRepository) ListByTenant(ctx context.Context, tenantID string) ([]execution.AsyncDecisionExecution, error) {
	const stmt = `
		select id, tenant_id, coalesce(scenario_id::text, ''), object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at,
			request_body, result_body, coalesce(callback_url, ''), coalesce(callback_status, ''), callback_attempt_count,
			coalesce(callback_last_error, ''), callback_sent_at, created_at, completed_at, failed_at
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
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt,
			&item.RequestBody, &item.ResultBody, &item.CallbackURL, &item.CallbackStatus, &item.CallbackAttemptCount,
			&item.CallbackLastError, &item.CallbackSentAt, &item.CreatedAt, &item.CompletedAt, &item.FailedAt,
		); err != nil {
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
		select id, tenant_id, coalesce(scenario_id::text, ''), object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at,
			request_body, result_body, coalesce(callback_url, ''), coalesce(callback_status, ''), callback_attempt_count,
			coalesce(callback_last_error, ''), callback_sent_at, created_at, completed_at, failed_at
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
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt,
			&item.RequestBody, &item.ResultBody, &item.CallbackURL, &item.CallbackStatus, &item.CallbackAttemptCount,
			&item.CallbackLastError, &item.CallbackSentAt, &item.CreatedAt, &item.CompletedAt, &item.FailedAt,
		); err != nil {
			return nil, err
		}
		item.Status = execution.Status(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r AsyncDecisionExecutionRepository) StartAttempt(ctx context.Context, id string) (execution.AsyncDecisionExecution, error) {
	const stmt = `
		update core.async_decision_executions
		set status = 'running',
		    attempt_count = attempt_count + 1,
		    next_attempt_at = null
		where id = $1 and status = 'queued'
		returning id, tenant_id, coalesce(scenario_id::text, ''), object_type, status, coalesce(idempotency_key, ''), attempt_count, max_attempts, next_attempt_at,
			request_body, result_body, coalesce(callback_url, ''), coalesce(callback_status, ''), callback_attempt_count,
			coalesce(callback_last_error, ''), callback_sent_at, created_at, completed_at, failed_at
	`
	var item execution.AsyncDecisionExecution
	var status string
	err := r.q.QueryRow(ctx, stmt, id).
		Scan(
			&item.ID, &item.TenantID, &item.ScenarioID, &item.ObjectType, &status, &item.IdempotencyKey, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt,
			&item.RequestBody, &item.ResultBody, &item.CallbackURL, &item.CallbackStatus, &item.CallbackAttemptCount,
			&item.CallbackLastError, &item.CallbackSentAt, &item.CreatedAt, &item.CompletedAt, &item.FailedAt,
		)
	item.Status = execution.Status(status)
	return item, err
}

func (r AsyncDecisionExecutionRepository) UpdateStatus(ctx context.Context, id string, status execution.Status) error {
	const stmt = `update core.async_decision_executions set status = $1, next_attempt_at = null, failed_at = case when $1 = 'failed' then now() else failed_at end where id = $2`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}

func (r AsyncDecisionExecutionRepository) MarkCompleted(ctx context.Context, id string, resultBody []byte, completedAt time.Time, callbackStatus string) error {
	const stmt = `
		update core.async_decision_executions
		set status = 'completed',
		    next_attempt_at = null,
		    last_error = '',
		    result_body = $1,
		    completed_at = $2,
		    failed_at = null,
		    callback_status = $3
		where id = $4
	`
	_, err := r.q.Exec(ctx, stmt, resultBody, completedAt, nullableEmptyString(callbackStatus), id)
	return err
}

func (r AsyncDecisionExecutionRepository) RecordAttemptFailure(ctx context.Context, id string, status execution.Status, nextAttemptAt *time.Time, lastError string, failedAt *time.Time) error {
	const stmt = `
		update core.async_decision_executions
		set status = $1,
		    next_attempt_at = $2,
		    last_error = $3,
		    failed_at = $4,
		    completed_at = null
		where id = $5
	`
	_, err := r.q.Exec(ctx, stmt, string(status), nextAttemptAt, lastError, failedAt, id)
	return err
}

func (r AsyncDecisionExecutionRepository) UpdateCallbackDelivery(ctx context.Context, id string, callbackStatus string, attemptCount int, lastError string, sentAt *time.Time) error {
	const stmt = `
		update core.async_decision_executions
		set callback_status = $1,
		    callback_attempt_count = $2,
		    callback_last_error = $3,
		    callback_sent_at = $4
		where id = $5
	`
	_, err := r.q.Exec(ctx, stmt, nullableEmptyString(callbackStatus), attemptCount, lastError, sentAt, id)
	return err
}

func (r AsyncDecisionExecutionRepository) ResetForRetry(ctx context.Context, id string, status execution.Status) error {
	const stmt = `
		update core.async_decision_executions
		set status = $1,
		    attempt_count = 0,
		    next_attempt_at = null,
		    last_error = '',
		    result_body = null,
		    completed_at = null,
		    failed_at = null,
		    callback_status = null,
		    callback_attempt_count = 0,
		    callback_last_error = '',
		    callback_sent_at = null
		where id = $2
	`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}
