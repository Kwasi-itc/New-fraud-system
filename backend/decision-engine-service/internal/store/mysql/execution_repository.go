package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/url"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type ScheduledExecutionRepository struct{ executionRepository }
type AsyncDecisionExecutionRepository struct{ executionRepository }

func (r ScheduledExecutionRepository) selectSQL() string {
	return "SELECT e.id,e.tenant_id,e.scenario_id,e.scenario_iteration_id,e.source,e.status,e.generation,e.attempt_count,e.max_attempts,e.scheduled_for,e.next_attempt_at,e.created_at,e.failed_at,e.cancelled_at," + r.project([]string{"e.idempotency_key", "e.last_error"}, "e.request_body") + " FROM " + r.schema + ".decision_engine_scheduled_executions e"
}
func (r ScheduledExecutionRepository) scan(row rowScanner) (ScheduledExecutionRecord, int64, error) {
	var v ScheduledExecutionRecord
	var id, tenant, parent, iteration mysqlstore.UUID
	var source, status string
	var scheduled, created mysqlstore.DateTime
	var next, failed, cancelled mysqlstore.NullDateTime
	var key, last mysqlstore.Text
	var body mysqlstore.JSON
	var size int64
	if err := row.Scan(&id, &tenant, &parent, &iteration, &source, &status, &v.Generation, &v.AttemptCount, &v.MaxAttempts, &scheduled, &next, &created, &failed, &cancelled, &size, &key, &last, &body); err != nil {
		return v, 0, err
	}
	v.ID = uuid.UUID(id).String()
	v.TenantID = uuid.UUID(tenant).String()
	v.ScenarioID = uuid.UUID(parent).String()
	v.ScenarioIterationID = uuid.UUID(iteration).String()
	v.Source = execution.Source(source)
	v.Status = execution.Status(status)
	v.ScheduledFor = scheduled.Time
	v.CreatedAt = created.Time
	v.NextAttemptAt = datePointer(next)
	v.FailedAt = datePointer(failed)
	v.CancelledAt = datePointer(cancelled)
	v.IdempotencyKey = string(key)
	v.LastError = string(last)
	v.RequestBody = json.RawMessage(body)
	return v, size, nil
}

func (r AsyncDecisionExecutionRepository) selectSQL() string {
	// SQL NULL result_body remains distinct from the JSON document null.
	return "SELECT e.id,e.tenant_id,l.scenario_id,e.status,e.generation,e.attempt_count,e.max_attempts,e.callback_status,e.callback_attempt_count,e.callback_max_attempts,e.next_attempt_at,e.callback_sent_at,e.created_at,e.completed_at,e.failed_at,e.cancelled_at,e.callback_cancelled_at," + r.project([]string{"e.object_type", "e.idempotency_key", "e.last_error", "e.callback_url", "e.callback_last_error"}, "e.request_body", "COALESCE(e.result_body,CAST('null' AS JSON))") + ",e.result_body IS NULL FROM " + r.schema + ".decision_engine_async_executions e LEFT JOIN " + r.schema + ".decision_engine_async_scenario_links l ON l.tenant_id=e.tenant_id AND l.execution_id=e.id"
}
func (r AsyncDecisionExecutionRepository) scan(row rowScanner) (AsyncExecutionRecord, int64, error) {
	var v AsyncExecutionRecord
	var id, tenant mysqlstore.UUID
	var parent mysqlstore.NullUUID
	var status, callback string
	var created mysqlstore.DateTime
	var next, sent, completed, failed, cancelled, cbCancelled mysqlstore.NullDateTime
	var object, key, last, url, cbLast mysqlstore.Text
	var request, result mysqlstore.JSON
	var absent mysqlstore.Bool
	var size int64
	if err := row.Scan(&id, &tenant, &parent, &status, &v.Generation, &v.AttemptCount, &v.MaxAttempts, &callback, &v.CallbackAttemptCount, &v.CallbackMaxAttempts, &next, &sent, &created, &completed, &failed, &cancelled, &cbCancelled, &size, &object, &key, &last, &url, &cbLast, &request, &result, &absent); err != nil {
		return v, 0, err
	}
	v.ID = uuid.UUID(id).String()
	v.TenantID = uuid.UUID(tenant).String()
	if parent.Valid {
		v.ScenarioID = uuid.UUID(parent.UUID).String()
	}
	v.ObjectType = string(object)
	v.Status = execution.Status(status)
	v.CallbackStatus = callback
	v.NextAttemptAt = datePointer(next)
	v.CallbackSentAt = datePointer(sent)
	v.CreatedAt = created.Time
	v.CompletedAt = datePointer(completed)
	v.FailedAt = datePointer(failed)
	v.CancelledAt, v.CallbackCancelledAt = datePointer(cancelled), datePointer(cbCancelled)
	v.IdempotencyKey = string(key)
	v.LastError = string(last)
	v.CallbackURL = string(url)
	v.CallbackLastError = string(cbLast)
	v.RequestBody = json.RawMessage(request)
	if !bool(absent) {
		v.ResultBody = json.RawMessage(result)
	}
	return v, size, nil
}
func (r ScheduledExecutionRepository) GetByID(ctx context.Context, tenant, parent, id string) (ScheduledExecutionRecord, error) {
	args, err := r.ids(tenant, parent, id)
	if err != nil {
		return ScheduledExecutionRecord{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE e.tenant_id=? AND e.scenario_id=? AND e.id=?", args...), r.scan)
}
func (r ScheduledExecutionRepository) get(ctx context.Context, id uuid.UUID) (ScheduledExecutionRecord, error) {
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE e.tenant_id=? AND e.id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), r.scan)
}
func (r AsyncDecisionExecutionRepository) GetByID(ctx context.Context, tenant, id string) (AsyncExecutionRecord, error) {
	args, err := r.ids(tenant, id)
	if err != nil {
		return AsyncExecutionRecord{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE e.tenant_id=? AND e.id=?", args...), r.scan)
}
func (r ScheduledExecutionRepository) ListByScenario(ctx context.Context, tenant, parent string) ([]ScheduledExecutionRecord, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE e.tenant_id=? AND e.scenario_id=? ORDER BY e.scheduled_for DESC,e.created_at DESC,e.id DESC", args, r.scan)
}
func (r AsyncDecisionExecutionRepository) ListByTenant(ctx context.Context, tenant string) ([]AsyncExecutionRecord, error) {
	args, err := r.ids(tenant)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE e.tenant_id=? ORDER BY e.created_at DESC,e.id DESC", args, r.scan)
}
func (r executionRepository) counts(ctx context.Context, table string, parent *string) (map[execution.Status]int, error) {
	args := []any{mysqlstore.UUID(r.tenant)}
	where := " WHERE tenant_id=?"
	if parent != nil {
		p, err := uuid.Parse(*parent)
		if err != nil || p == uuid.Nil {
			return nil, ErrTenantScope
		}
		where += " AND scenario_id=?"
		args = append(args, mysqlstore.UUID(p))
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT status,COUNT(*) FROM "+r.schema+"."+table+where+" GROUP BY status", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[execution.Status]int{}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		if n < 0 || int64(int(n)) != n {
			return nil, errors.New("execution count exceeds public count range")
		}
		out[execution.Status(status)] = int(n)
	}
	return out, rows.Err()
}
func (r ScheduledExecutionRepository) CountByStatus(ctx context.Context, tenant, parent string) (map[execution.Status]int, error) {
	if err := r.scope(tenant); err != nil {
		return nil, err
	}
	return r.counts(ctx, executionOwner(ports.JobScheduled), &parent)
}
func (r AsyncDecisionExecutionRepository) CountByStatus(ctx context.Context, tenant string) (map[execution.Status]int, error) {
	if err := r.scope(tenant); err != nil {
		return nil, err
	}
	return r.counts(ctx, executionOwner(ports.JobAsync), nil)
}

func (r ScheduledExecutionRepository) values(v execution.ScheduledExecution) ([]any, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.IdempotencyKey, v.LastError)
	if err != nil {
		return nil, err
	}
	args, err := r.ids(v.TenantID, v.ScenarioID, v.ScenarioIterationID)
	if err != nil {
		return nil, err
	}
	if (v.Source != execution.SourceManual && v.Source != execution.SourceRecurring) || v.Status != execution.StatusPending || v.AttemptCount != 0 || v.MaxAttempts < 1 || v.MaxAttempts > math.MaxInt32 || v.NextAttemptAt != nil || v.FailedAt != nil || v.LastError != "" {
		return nil, ErrExecutionState
	}
	if _, err := (mysqlstore.DateTime{Time: v.ScheduledFor}).Value(); err != nil {
		return nil, err
	}
	if len(v.IdempotencyKey)+len(v.RequestBody) > r.maxBytes {
		return nil, ErrProjectionLimit
	}
	body := mysqlstore.JSON(v.RequestBody)
	if _, err := body.Value(); err != nil {
		return nil, err
	}
	return []any{id, args[0], args[1], args[2], string(v.Source), string(v.Status), mysqlstore.Text(v.IdempotencyKey), v.MaxAttempts, mysqlstore.DateTime{Time: v.ScheduledFor}, body, mysqlstore.Text(""), mysqlstore.DateTime{Time: v.CreatedAt}}, nil
}

const scheduledInsertColumns = "id,tenant_id,scenario_id,scenario_iteration_id,source,status,idempotency_key,max_attempts,scheduled_for,request_body,last_error,created_at"

func (r ScheduledExecutionRepository) Create(ctx context.Context, v execution.ScheduledExecution) (out ScheduledCreation, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRequest, ExecutionScheduleTick); err != nil {
		return out, err
	}
	values, err := r.values(v)
	if err != nil {
		return out, err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_scheduled_executions("+scheduledInsertColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", values...)
	if err != nil {
		if mysqlstore.Classify(err) != mysqlstore.Duplicate {
			return out, err
		}
		found, e := r.replay(ctx, v)
		if e != nil {
			return out, errors.Join(err, e)
		}
		out.Record = found
		out.Replay = true
		return out, nil
	}
	out.Record, err = r.get(ctx, uuid.MustParse(v.ID))
	if err != nil {
		return out, err
	}
	err = r.enqueueScheduled(ctx, out.Record)
	return out, err
}

// Replay reads are current shared reads: they see the committed unique-key
// winner under RC without S->X lock upgrades among concurrent duplicate callers.
// The original request wins, matching source behavior. Conflicting ID/key/slot
// identities fail instead of returning an arbitrary OR-query candidate.
func (r ScheduledExecutionRepository) replay(ctx context.Context, v execution.ScheduledExecution) (ScheduledExecutionRecord, error) {
	var candidates []ScheduledExecutionRecord
	read := func(where string, args ...any) error {
		x, err := platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE e.tenant_id=? AND "+where+" FOR SHARE OF e", append([]any{mysqlstore.UUID(r.tenant)}, args...)...), r.scan)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err == nil {
			candidates = append(candidates, x)
		}
		return err
	}
	if err := read("e.id=?", mysqlstore.UUID(uuid.MustParse(v.ID))); err != nil {
		return ScheduledExecutionRecord{}, err
	}
	if len(candidates) > 0 {
		x := candidates[0]
		if !(v.IdempotencyKey != "" && x.IdempotencyKey == v.IdempotencyKey) && !(v.Source == execution.SourceRecurring && x.Source == execution.SourceRecurring && x.ScenarioID == v.ScenarioID && x.ScheduledFor.Equal(v.ScheduledFor)) {
			return ScheduledExecutionRecord{}, ErrExecutionReplayConflict
		}
	}
	if v.IdempotencyKey != "" {
		if err := read("e.idempotency_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(e.idempotency_key AS BINARY)=CAST(? AS BINARY)", mysqlstore.Text(v.IdempotencyKey), mysqlstore.Text(v.IdempotencyKey)); err != nil {
			return ScheduledExecutionRecord{}, err
		}
	}
	if v.Source == execution.SourceRecurring {
		if err := read("e.scenario_id=? AND e.recurrence_slot=?", mysqlstore.UUID(uuid.MustParse(v.ScenarioID)), mysqlstore.DateTime{Time: v.ScheduledFor}); err != nil {
			return ScheduledExecutionRecord{}, err
		}
	}
	if len(candidates) == 0 {
		return ScheduledExecutionRecord{}, ErrExecutionReplayConflict
	}
	for _, x := range candidates {
		if x.ID != candidates[0].ID {
			return ScheduledExecutionRecord{}, ErrExecutionReplayConflict
		}
	}
	return candidates[0], nil
}

func (r AsyncDecisionExecutionRepository) Create(ctx context.Context, v execution.AsyncDecisionExecution) (out AsyncCreation, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRequest); err != nil {
		return out, err
	}
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.ObjectType, v.IdempotencyKey, v.CallbackURL)
	if err != nil {
		return out, err
	}
	if v.Status != execution.StatusQueued || v.AttemptCount != 0 || v.MaxAttempts < 1 || v.MaxAttempts > math.MaxInt32 || v.NextAttemptAt != nil || v.ResultBody != nil || v.LastError != "" || v.CallbackStatus != "" || v.CallbackAttemptCount != 0 || v.CallbackLastError != "" || v.CallbackSentAt != nil || v.CompletedAt != nil || v.FailedAt != nil {
		return out, ErrExecutionState
	}
	if v.ObjectType == "" || len(v.ObjectType)+len(v.IdempotencyKey)+len(v.CallbackURL)+len(v.RequestBody) > r.maxBytes {
		return out, ErrProjectionLimit
	}
	if v.CallbackURL != "" {
		parsed, e := url.Parse(v.CallbackURL)
		if e != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return out, errors.New("callback URL requires an absolute http or https host")
		}
	}
	if v.ScenarioID != "" {
		if _, err = r.ids(v.TenantID, v.ScenarioID); err != nil {
			return out, err
		}
	}
	body := mysqlstore.JSON(v.RequestBody)
	if _, err = body.Value(); err != nil {
		return out, err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_async_executions(id,tenant_id,object_type,status,idempotency_key,max_attempts,request_body,last_error,callback_url,callback_max_attempts,callback_last_error,created_at) VALUES(?,?,?,'queued',?,?,?,'',?,?,'',?)", id, mysqlstore.UUID(r.tenant), mysqlstore.Text(v.ObjectType), mysqlstore.Text(v.IdempotencyKey), v.MaxAttempts, body, mysqlstore.Text(v.CallbackURL), r.policy.CallbackMaxAttempts, mysqlstore.DateTime{Time: v.CreatedAt})
	if err != nil {
		if mysqlstore.Classify(err) != mysqlstore.Duplicate || v.IdempotencyKey == "" {
			return out, err
		}
		original := err
		// Reject a conflicting same-tenant explicit ID before recovering by key.
		var key mysqlstore.Text
		e := r.tx.QueryRowContext(ctx, "SELECT idempotency_key FROM "+r.schema+".decision_engine_async_executions WHERE tenant_id=? AND id=? FOR SHARE", mysqlstore.UUID(r.tenant), id).Scan(&key)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if e == nil && string(key) != v.IdempotencyKey {
			return out, ErrExecutionReplayConflict
		}
		out.Record, err = platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE e.tenant_id=? AND e.idempotency_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(e.idempotency_key AS BINARY)=CAST(? AS BINARY) FOR SHARE OF e", mysqlstore.UUID(r.tenant), mysqlstore.Text(v.IdempotencyKey), mysqlstore.Text(v.IdempotencyKey)), r.scan)
		if err != nil {
			return out, errors.Join(original, err)
		}
		out.Replay = true
		return out, nil
	}
	if v.ScenarioID != "" {
		_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_async_scenario_links(tenant_id,execution_id,scenario_id) VALUES(?,?,?)", mysqlstore.UUID(r.tenant), id, mysqlstore.UUID(uuid.MustParse(v.ScenarioID)))
		if err != nil {
			return out, err
		}
	}
	out.Record, err = r.GetByID(ctx, v.TenantID, v.ID)
	if err != nil {
		return out, err
	}
	err = r.enqueueAsync(ctx, out.Record, ports.JobAsync, nil)
	return out, err
}
