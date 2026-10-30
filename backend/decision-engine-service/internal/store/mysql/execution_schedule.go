package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/recurrence"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

type ExecutionScheduleRepository struct{ executionRepository }

// A definition is immutable; replacement deactivates the old definition and
// creates a new ID in the same admin transaction. Inactive history is retained.
// Interval definitions require StartAt. Calendar definitions require Calendar
// and StartLocal instead; their physical StartAt is derived, never guessed.
// There is no implicit run-on-start occurrence. Calendar is copied on write.
type ExecutionScheduleInput struct {
	ID, ScenarioID, ScenarioIterationID uuid.UUID
	StartAt                             time.Time
	Interval                            time.Duration
	Calendar                            *recurrence.Spec
	StartLocal                          string
	Policy                              workers.MissedRunPolicy
	LatenessWindow                      time.Duration
	MaxAttempts                         int
	RequestBody                         json.RawMessage
	Active                              bool
}

type ExecutionScheduleRecord struct {
	ExecutionScheduleInput
	TenantID                       uuid.UUID
	NextAt, CreatedAt, UpdatedAt   time.Time
	SkippedCount, WatermarkVersion int64
	NextLocal                      string
}

type MaterializedExecutions struct {
	Records []ScheduledExecutionRecord
	workers.OccurrencePlan
	WatermarkVersion int64
	NextLocal        string
}

func (r ExecutionScheduleRepository) selectSQL() string {
	return "SELECT id,tenant_id,scenario_id,scenario_iteration_id,start_at,next_at,interval_us,missed_policy,lateness_us,max_attempts,active,skipped_count,watermark_version,created_at,updated_at,schedule_kind,start_local,next_local," + r.project(nil, "request_body", "COALESCE(calendar_spec,JSON_OBJECT())") + " FROM " + r.schema + ".decision_engine_execution_schedules"
}
func (r ExecutionScheduleRepository) scan(row rowScanner) (ExecutionScheduleRecord, int64, error) {
	var v ExecutionScheduleRecord
	var id, tenant, parent, iteration mysqlstore.UUID
	var start, next, created, updated mysqlstore.DateTime
	var interval, late, size int64
	var policy, kind string
	var startLocal, nextLocal sql.NullString
	var active mysqlstore.Bool
	var body, calendarJSON mysqlstore.JSON
	err := row.Scan(&id, &tenant, &parent, &iteration, &start, &next, &interval, &policy, &late, &v.MaxAttempts, &active, &v.SkippedCount, &v.WatermarkVersion, &created, &updated, &kind, &startLocal, &nextLocal, &size, &body, &calendarJSON)
	if err != nil {
		return v, 0, err
	}
	if size > int64(r.maxBytes) {
		return v, size, nil // platformOne/List report the projection failure.
	}
	if interval < 0 || interval > math.MaxInt64/int64(time.Microsecond) || late < 0 || late > math.MaxInt64/int64(time.Microsecond) {
		return v, 0, ErrExecutionState
	}
	v.ID, v.TenantID, v.ScenarioID, v.ScenarioIterationID = uuid.UUID(id), uuid.UUID(tenant), uuid.UUID(parent), uuid.UUID(iteration)
	v.StartAt, v.NextAt, v.CreatedAt, v.UpdatedAt = start.Time, next.Time, created.Time, updated.Time
	v.Interval, v.LatenessWindow, v.Policy, v.Active, v.RequestBody = time.Duration(interval)*time.Microsecond, time.Duration(late)*time.Microsecond, workers.MissedRunPolicy(policy), bool(active), json.RawMessage(body)
	switch kind {
	case "interval":
		if interval == 0 || startLocal.Valid || nextLocal.Valid {
			return v, 0, ErrExecutionState
		}
	case "calendar":
		if interval != 0 || !startLocal.Valid || !nextLocal.Valid {
			return v, 0, ErrExecutionState
		}
		var spec recurrence.Spec
		decoder := json.NewDecoder(bytes.NewReader(calendarJSON))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&spec); err != nil {
			return v, 0, err
		}
		c, err := recurrence.New(spec)
		if err != nil {
			return v, 0, err
		}
		if err := c.ValidatePolicy(v.Policy, v.LatenessWindow); err != nil {
			return v, 0, err
		}
		if _, err := c.ParseSlot(startLocal.String); err != nil {
			return v, 0, err
		}
		n, err := c.ParseSlot(nextLocal.String)
		if err != nil {
			return v, 0, err
		}
		at, exists, err := c.Resolve(n)
		if err != nil || !exists || !at.Equal(v.NextAt) {
			return v, 0, ErrExecutionState
		}
		v.Calendar, v.StartLocal, v.NextLocal = &spec, startLocal.String, nextLocal.String
	default:
		return v, 0, ErrExecutionState
	}
	return v, size, nil
}
func (r ExecutionScheduleRepository) Get(ctx context.Context, id uuid.UUID) (ExecutionScheduleRecord, error) {
	if id == uuid.Nil {
		return ExecutionScheduleRecord{}, ErrTenantScope
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), r.scan)
}
func (r ExecutionScheduleRepository) lock(ctx context.Context, id uuid.UUID) (ExecutionScheduleRecord, error) {
	if id == uuid.Nil {
		return ExecutionScheduleRecord{}, ErrTenantScope
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), r.scan)
}

func (r ExecutionScheduleRepository) Create(ctx context.Context, v ExecutionScheduleInput) (out ExecutionScheduleRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionScheduleAdmin); err != nil {
		return out, err
	}
	if v.ID == uuid.Nil || v.ScenarioID == uuid.Nil || v.ScenarioIterationID == uuid.Nil || v.MaxAttempts < 1 || v.MaxAttempts > math.MaxInt32 {
		return out, ErrExecutionState
	}
	kind := "interval"
	var calendarJSON mysqlstore.NullJSON
	var startLocal, nextLocal sql.NullString
	var skipped int64
	if v.Calendar == nil {
		if v.StartLocal != "" {
			return out, ErrExecutionState
		}
		if _, err = workers.PlanOccurrences(workers.IntervalSchedule{NextAt: v.StartAt, Interval: v.Interval, Policy: v.Policy, LatenessWindow: v.LatenessWindow}, v.StartAt, 1); err != nil {
			return out, err
		}
	} else {
		if !v.StartAt.IsZero() || v.Interval != 0 {
			return out, ErrExecutionState
		}
		c, calendarErr := recurrence.New(*v.Calendar)
		if calendarErr != nil {
			return out, calendarErr
		}
		if err = c.ValidatePolicy(v.Policy, v.LatenessWindow); err != nil {
			return out, err
		}
		anchor, calendarErr := c.ParseSlot(v.StartLocal)
		if calendarErr != nil {
			return out, calendarErr
		}
		first, at, gaps, calendarErr := c.First(anchor)
		if calendarErr != nil {
			return out, calendarErr
		}
		raw, calendarErr := json.Marshal(c.Spec())
		if calendarErr != nil {
			return out, calendarErr
		}
		kind, v.StartAt, skipped = "calendar", at, gaps
		calendarJSON = mysqlstore.NullJSON{Valid: true, JSON: mysqlstore.JSON(raw)}
		startLocal, nextLocal = sql.NullString{String: v.StartLocal, Valid: true}, sql.NullString{String: first.String(), Valid: true}
	}
	if len(v.RequestBody)+len(calendarJSON.JSON) > r.maxBytes {
		return out, ErrProjectionLimit
	}
	if _, err = mysqlstore.JSON(v.RequestBody).Value(); err != nil {
		return out, err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_execution_schedules(id,tenant_id,scenario_id,scenario_iteration_id,start_at,next_at,schedule_kind,interval_us,calendar_spec,start_local,next_local,missed_policy,lateness_us,max_attempts,request_body,active,skipped_count,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(v.ID), mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.ScenarioID), mysqlstore.UUID(v.ScenarioIterationID), mysqlstore.DateTime{Time: v.StartAt}, mysqlstore.DateTime{Time: v.StartAt}, kind, v.Interval.Microseconds(), calendarJSON, startLocal, nextLocal, string(v.Policy), v.LatenessWindow.Microseconds(), v.MaxAttempts, mysqlstore.JSON(v.RequestBody), mysqlstore.Bool(v.Active), skipped)
	if err != nil {
		return out, err
	}
	return r.Get(ctx, v.ID)
}

func (r ExecutionScheduleRepository) SetActive(ctx context.Context, id uuid.UUID, version int64, active bool) (err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionScheduleAdmin); err != nil {
		return err
	}
	v, err := r.lock(ctx, id)
	if err != nil {
		return err
	}
	if v.WatermarkVersion != version || version == math.MaxInt64 {
		return ErrExecutionState
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_execution_schedules SET active=?,watermark_version=watermark_version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND watermark_version=?", mysqlstore.Bool(active), mysqlstore.UUID(r.tenant), mysqlstore.UUID(id), version)
	return err
}

type ScheduleCursor struct {
	NextAt time.Time
	ID     uuid.UUID
}

// Due reads only an indexed tenant's next page of identities, never execution
// histories or an arbitrary subset from which to infer due relationships.
func (r ExecutionScheduleRepository) Due(ctx context.Context, after *ScheduleCursor, limit int) ([]ScheduleCursor, error) {
	if r.action != ExecutionScheduleTick || limit < 1 || limit > min(r.maxRows, 100) {
		return nil, ErrDecisionAuthorizationDenied
	}
	query := "SELECT next_at,id FROM " + r.schema + ".decision_engine_execution_schedules WHERE tenant_id=? AND active=1 AND next_at<=UTC_TIMESTAMP(6)"
	args := []any{mysqlstore.UUID(r.tenant)}
	if after != nil {
		if after.ID == uuid.Nil {
			return nil, ErrTenantScope
		}
		if _, err := (mysqlstore.DateTime{Time: after.NextAt}).Value(); err != nil {
			return nil, err
		}
		query += " AND(next_at>? OR(next_at=? AND id>?))"
		args = append(args, mysqlstore.DateTime{Time: after.NextAt}, mysqlstore.DateTime{Time: after.NextAt}, mysqlstore.UUID(after.ID))
	}
	args = append(args, limit)
	rows, err := r.tx.QueryContext(ctx, query+" ORDER BY next_at,id LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduleCursor
	for rows.Next() {
		var at mysqlstore.DateTime
		var id mysqlstore.UUID
		if err := rows.Scan(&at, &id); err != nil {
			return nil, err
		}
		out = append(out, ScheduleCursor{at.Time, uuid.UUID(id)})
	}
	return out, rows.Err()
}

// Materialize persists occurrence receipts, new executions, required jobs and
// the watermark in ONE transaction. The schedule row is the narrow lock; no
// network work occurs here. Call again for MoreDue; do not retry ambiguous commit
// errors blindly. The stored watermark and receipts are reconciliation sources.
func (r ExecutionScheduleRepository) Materialize(ctx context.Context, id uuid.UUID, limit int) (out MaterializedExecutions, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionScheduleTick); err != nil {
		return out, err
	}
	if limit < 1 || limit > min(r.maxRows, 100) {
		return out, ErrProjectionLimit
	}
	v, err := r.lock(ctx, id)
	if err != nil {
		return out, err
	}
	if !v.Active {
		return out, ErrExecutionState
	}
	// Publication writes this link in its own short transaction. The shared
	// lock pins the published iteration only for this bounded materialization.
	var live mysqlstore.UUID
	err = r.tx.QueryRowContext(ctx, "SELECT iteration_id FROM "+r.schema+".decision_engine_scenario_live_iterations WHERE tenant_id=? AND scenario_id=? FOR SHARE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.ScenarioID)).Scan(&live)
	if err != nil {
		return out, err
	}
	if uuid.UUID(live) != v.ScenarioIterationID {
		return out, ErrExecutionState
	}
	now, err := r.databaseTime(ctx)
	if err != nil {
		return out, err
	}
	if v.Calendar == nil {
		out.OccurrencePlan, err = workers.PlanOccurrences(workers.IntervalSchedule{NextAt: v.NextAt, Interval: v.Interval, Policy: v.Policy, LatenessWindow: v.LatenessWindow}, now, limit)
	} else {
		var c *recurrence.Calendar
		c, err = recurrence.New(*v.Calendar)
		if err == nil {
			var cursor recurrence.Slot
			cursor, err = c.ParseSlot(v.NextLocal)
			if err == nil {
				var planned recurrence.Plan
				planned, err = c.PlanOccurrences(cursor, now, v.Policy, v.LatenessWindow, limit)
				out.OccurrencePlan, out.NextLocal = planned.OccurrencePlan, planned.NextLocal
			}
		}
	}
	if err != nil {
		return out, err
	}
	out.WatermarkVersion = v.WatermarkVersion
	if out.NextAt.Equal(v.NextAt) {
		return out, nil
	}
	if v.WatermarkVersion == math.MaxInt64 || out.Skipped > math.MaxInt64-v.SkippedCount {
		return out, ErrExecutionState
	}
	out.Records, err = r.materializeRecords(ctx, v, out.Occurrences, now)
	if err != nil {
		return out, err
	}
	var cursor any
	if v.Calendar != nil {
		cursor = out.NextLocal
	}
	result, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_execution_schedules SET next_at=?,next_local=?,skipped_count=skipped_count+?,watermark_version=watermark_version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND watermark_version=?", mysqlstore.DateTime{Time: out.NextAt}, cursor, out.Skipped, mysqlstore.UUID(r.tenant), mysqlstore.UUID(id), v.WatermarkVersion)
	err = executionChanged(result, err)
	if err == nil {
		out.WatermarkVersion++
	}
	return out, err
}

func (r ExecutionScheduleRepository) materializeRecords(ctx context.Context, v ExecutionScheduleRecord, times []time.Time, now time.Time) ([]ScheduledExecutionRecord, error) {
	if len(times) == 0 {
		return nil, nil
	}
	repo := ScheduledExecutionRepository{r.executionRepository}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(times)), ",")
	args := []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.ScenarioID)}
	for _, at := range times {
		args = append(args, mysqlstore.DateTime{Time: at})
	}
	query := repo.selectSQL() + " WHERE e.tenant_id=? AND e.scenario_id=? AND e.recurrence_slot IN(" + marks + ")"
	existing, err := platformList(ctx, r.platformRepository, query, args, repo.scan)
	if err != nil {
		return nil, err
	}
	byTime := map[int64]ScheduledExecutionRecord{}
	for _, record := range existing {
		// An old definition may own this unique scenario occurrence. Do not
		// silently retarget an already scheduled execution to a new iteration.
		if record.ScenarioIterationID != v.ScenarioIterationID.String() {
			return nil, ErrExecutionReplayConflict
		}
		byTime[record.ScheduledFor.UnixMicro()] = record
	}
	var inserts []string
	var insertArgs []any
	var jobs []executionJob
	var projectedBytes int64
	for _, record := range existing {
		projectedBytes += int64(len(record.IdempotencyKey) + len(record.LastError) + len(record.RequestBody))
	}
	for _, at := range times {
		if _, ok := byTime[at.UnixMicro()]; ok {
			continue
		}
		x := execution.ScheduledExecution{ID: uuid.NewString(), TenantID: r.tenant.String(), ScenarioID: v.ScenarioID.String(), ScenarioIterationID: v.ScenarioIterationID.String(), Source: execution.SourceRecurring, Status: execution.StatusPending, MaxAttempts: v.MaxAttempts, ScheduledFor: at, CreatedAt: now, RequestBody: v.RequestBody}
		if int64(len(x.RequestBody)) > int64(r.maxBytes)-projectedBytes {
			return nil, ErrProjectionLimit
		}
		projectedBytes += int64(len(x.RequestBody))
		values, err := repo.values(x)
		if err != nil {
			return nil, err
		}
		inserts = append(inserts, "(?,?,?,?,?,?,?,?,?,?,?,?)")
		insertArgs = append(insertArgs, values...)
		byTime[at.UnixMicro()] = ScheduledExecutionRecord{ScheduledExecution: x}
		when := at
		jobs = append(jobs, executionJob{mysqljobs.Identity{Version: 1, Kind: ports.JobScheduled, TenantID: r.tenant, ResourceID: uuid.MustParse(x.ID), DomainAttempt: 1}, &when})
	}
	if len(inserts) > 0 {
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_scheduled_executions("+scheduledInsertColumns+") VALUES"+strings.Join(inserts, ","), insertArgs...); err != nil {
			return nil, err
		}
		if err := r.enqueueExecutionJobs(ctx, jobs); err != nil {
			return nil, err
		}
	}
	// Read back in a single bounded query so JSON normalization/projection is
	// identical for newly inserted and reused occurrences. No per-row reads.
	records, err := platformList(ctx, r.platformRepository, query+" ORDER BY e.scheduled_for", args, repo.scan)
	if err != nil {
		return nil, err
	}
	if len(records) != len(times) {
		return nil, errors.New("materialized execution readback mismatch")
	}
	var receipts []string
	var receiptArgs []any
	for _, record := range records {
		receipts = append(receipts, "(?,?,?,?,UTC_TIMESTAMP(6))")
		receiptArgs = append(receiptArgs, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.ID), mysqlstore.DateTime{Time: record.ScheduledFor}, mysqlstore.UUID(uuid.MustParse(record.ID)))
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_schedule_occurrences(tenant_id,schedule_id,scheduled_for,execution_id,created_at) VALUES"+strings.Join(receipts, ","), receiptArgs...)
	return records, err
}
