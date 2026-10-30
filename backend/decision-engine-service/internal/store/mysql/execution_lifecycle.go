package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (r executionRepository) databaseTime(ctx context.Context) (time.Time, error) {
	var at mysqlstore.DateTime
	err := r.tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&at)
	return at.Time, err
}
func (r ScheduledExecutionRepository) locked(ctx context.Context, lease ExecutionLease) (ScheduledExecutionRecord, error) {
	if !r.validLease(lease, ports.JobScheduled) {
		return ScheduledExecutionRecord{}, ErrExecutionLease
	}
	if err := r.lockResource(ctx, ports.JobScheduled, lease.Identity.ResourceID); err != nil {
		return ScheduledExecutionRecord{}, err
	}
	v, err := r.get(ctx, lease.Identity.ResourceID)
	if err != nil {
		return v, err
	}
	if v.Generation != lease.Identity.RedriveGeneration {
		return v, ErrExecutionState
	}
	if err := r.lockLease(ctx, lease, ports.JobScheduled); err != nil {
		return v, err
	}
	return v, nil
}
func (r AsyncDecisionExecutionRepository) locked(ctx context.Context, lease ExecutionLease, kind ports.JobKind) (AsyncExecutionRecord, error) {
	if !r.validLease(lease, kind) {
		return AsyncExecutionRecord{}, ErrExecutionLease
	}
	if err := r.lockResource(ctx, kind, lease.Identity.ResourceID); err != nil {
		return AsyncExecutionRecord{}, err
	}
	v, err := r.GetByID(ctx, r.tenant.String(), lease.Identity.ResourceID.String())
	if err != nil {
		return v, err
	}
	if v.Generation != lease.Identity.RedriveGeneration {
		return v, ErrExecutionState
	}
	if err := r.lockLease(ctx, lease, kind); err != nil {
		return v, err
	}
	return v, nil
}

// Start runs before evaluation/network work, and returns provisional data until
// the owner transaction commits. A rescued transport may resume the SAME domain
// attempt; it must not consume another domain retry. Only the current SQL lease
// can do so. Full business-effect idempotency still needs the main handler wiring.
func (r ScheduledExecutionRepository) Start(ctx context.Context, lease ExecutionLease) (out ScheduledExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return out, err
	}
	out, err = r.locked(ctx, lease)
	if err != nil {
		return out, err
	}
	if out.Status == execution.StatusRunning && out.AttemptCount == lease.Identity.DomainAttempt {
		return out, r.lockLease(ctx, lease, ports.JobScheduled)
	}
	if out.Status != execution.StatusPending || out.AttemptCount >= out.MaxAttempts || out.AttemptCount == math.MaxInt32 || out.AttemptCount+1 != lease.Identity.DomainAttempt {
		return out, ErrExecutionState
	}
	now, err := r.databaseTime(ctx)
	if err != nil {
		return out, err
	}
	if out.ScheduledFor.After(now) || out.NextAttemptAt != nil && out.NextAttemptAt.After(now) {
		return out, ErrExecutionState
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scheduled_executions SET status='running',attempt_count=attempt_count+1,next_attempt_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return out, err
	}
	out.Status = execution.StatusRunning
	out.AttemptCount++
	out.NextAttemptAt = nil
	return out, r.lockLease(ctx, lease, ports.JobScheduled)
}
func (r AsyncDecisionExecutionRepository) Start(ctx context.Context, lease ExecutionLease) (out AsyncExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return out, err
	}
	out, err = r.locked(ctx, lease, ports.JobAsync)
	if err != nil {
		return out, err
	}
	if out.Status == execution.StatusRunning && out.AttemptCount == lease.Identity.DomainAttempt {
		return out, r.lockLease(ctx, lease, ports.JobAsync)
	}
	if out.Status != execution.StatusQueued || out.AttemptCount >= out.MaxAttempts || out.AttemptCount == math.MaxInt32 || out.AttemptCount+1 != lease.Identity.DomainAttempt {
		return out, ErrExecutionState
	}
	now, err := r.databaseTime(ctx)
	if err != nil {
		return out, err
	}
	if out.NextAttemptAt != nil && out.NextAttemptAt.After(now) {
		return out, ErrExecutionState
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET status='running',attempt_count=attempt_count+1,next_attempt_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return out, err
	}
	out.Status = execution.StatusRunning
	out.AttemptCount++
	out.NextAttemptAt = nil
	return out, r.lockLease(ctx, lease, ports.JobAsync)
}
func (r ScheduledExecutionRepository) Complete(ctx context.Context, lease ExecutionLease) (err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return err
	}
	v, err := r.locked(ctx, lease)
	if err != nil {
		return err
	}
	if v.Status != execution.StatusRunning || v.AttemptCount != lease.Identity.DomainAttempt {
		return ErrExecutionState
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scheduled_executions SET status='completed',next_attempt_at=NULL,last_error='',failed_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return err
	}
	return r.finishLease(ctx, lease)
}
func (r AsyncDecisionExecutionRepository) Complete(ctx context.Context, lease ExecutionLease, result []byte) (err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return err
	}
	if _, err = (mysqlstore.JSON(result)).Value(); err != nil {
		return err
	}
	if len(result) > r.maxBytes {
		return ErrProjectionLimit
	}
	v, err := r.locked(ctx, lease, ports.JobAsync)
	if err != nil {
		return err
	}
	if v.Status != execution.StatusRunning || v.AttemptCount != lease.Identity.DomainAttempt {
		return ErrExecutionState
	}
	callback := ""
	if v.CallbackURL != "" {
		callback = "pending"
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET status='completed',next_attempt_at=NULL,last_error='',result_body=?,completed_at=UTC_TIMESTAMP(6),failed_at=NULL,callback_status=? WHERE tenant_id=? AND id=?", mysqlstore.JSON(result), callback, mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return err
	}
	v, err = r.GetByID(ctx, v.TenantID, v.ID)
	if err != nil {
		return err
	}
	if callback != "" {
		if err = r.enqueueAsync(ctx, v, ports.JobCallback, nil); err != nil {
			return err
		}
	}
	return r.finishLease(ctx, lease)
}

// A nil next instant is an explicit terminal failure (including validation).
// Otherwise retry scheduling, owner state and the next exact domain job are one
// transaction. Storage never silently coerces an exhausted retry into success.
func (r ScheduledExecutionRepository) Fail(ctx context.Context, lease ExecutionLease, next *time.Time, message string) (err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return err
	}
	if message == "" {
		return ErrExecutionState
	}
	if err = r.scope(r.tenant.String(), message); err != nil {
		return err
	}
	at, err := optionalDate(next)
	if err != nil {
		return err
	}
	v, err := r.locked(ctx, lease)
	if err != nil {
		return err
	}
	if v.Status != execution.StatusRunning || v.AttemptCount != lease.Identity.DomainAttempt || next != nil && v.AttemptCount >= v.MaxAttempts {
		return ErrExecutionState
	}
	status := "failed"
	failed := "UTC_TIMESTAMP(6)"
	if next != nil {
		status = "pending"
		failed = "NULL"
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scheduled_executions SET status=?,next_attempt_at=?,last_error=?,failed_at="+failed+" WHERE tenant_id=? AND id=?", status, at, mysqlstore.Text(message), mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return err
	}
	v, err = r.get(ctx, lease.Identity.ResourceID)
	if err != nil {
		return err
	}
	if next != nil {
		if err = r.enqueueScheduled(ctx, v); err != nil {
			return err
		}
	}
	return r.finishLease(ctx, lease)
}
func (r AsyncDecisionExecutionRepository) Fail(ctx context.Context, lease ExecutionLease, next *time.Time, message string, result []byte) (err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return err
	}
	if message == "" {
		return ErrExecutionState
	}
	if err = r.scope(r.tenant.String(), message); err != nil {
		return err
	}
	if len(result) > r.maxBytes {
		return ErrProjectionLimit
	}
	body := mysqlstore.NullJSON{JSON: mysqlstore.JSON(result), Valid: result != nil}
	if _, err = body.Value(); err != nil {
		return err
	}
	if next != nil && result != nil {
		return ErrExecutionState
	}
	at, err := optionalDate(next)
	if err != nil {
		return err
	}
	v, err := r.locked(ctx, lease, ports.JobAsync)
	if err != nil {
		return err
	}
	if v.Status != execution.StatusRunning || v.AttemptCount != lease.Identity.DomainAttempt || next != nil && v.AttemptCount >= v.MaxAttempts {
		return ErrExecutionState
	}
	status := "failed"
	callback := ""
	failed := "UTC_TIMESTAMP(6)"
	if next != nil {
		status = "queued"
		failed = "NULL"
	} else if v.CallbackURL != "" {
		callback = "pending"
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET status=?,next_attempt_at=?,last_error=?,failed_at="+failed+",result_body=?,completed_at=NULL,callback_status=? WHERE tenant_id=? AND id=?", status, at, mysqlstore.Text(message), body, callback, mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return err
	}
	v, err = r.GetByID(ctx, v.TenantID, v.ID)
	if err != nil {
		return err
	}
	if next != nil {
		err = r.enqueueAsync(ctx, v, ports.JobAsync, next)
	} else if callback != "" {
		err = r.enqueueAsync(ctx, v, ports.JobCallback, nil)
	}
	if err != nil {
		return err
	}
	return r.finishLease(ctx, lease)
}

func (r AsyncDecisionExecutionRepository) StartCallback(ctx context.Context, lease ExecutionLease) (out AsyncExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return out, err
	}
	out, err = r.locked(ctx, lease, ports.JobCallback)
	if err != nil {
		return out, err
	}
	if out.CallbackURL == "" || !terminalExecutionStatus(out.Status) {
		return out, ErrExecutionState
	}
	if out.CallbackStatus == "running" && out.CallbackAttemptCount == lease.Identity.DomainAttempt {
		return out, r.lockLease(ctx, lease, ports.JobCallback)
	}
	if (out.CallbackStatus != "pending" && out.CallbackStatus != "failed") || out.CallbackAttemptCount >= out.CallbackMaxAttempts || out.CallbackAttemptCount == math.MaxInt32 || out.CallbackAttemptCount+1 != lease.Identity.DomainAttempt {
		return out, ErrExecutionState
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET callback_status='running',callback_attempt_count=callback_attempt_count+1 WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return out, err
	}
	out.CallbackStatus = "running"
	out.CallbackAttemptCount++
	return out, r.lockLease(ctx, lease, ports.JobCallback)
}

// HTTP delivery belongs AFTER StartCallback's transaction commits and BEFORE
// this outcome transaction. Lease/generation fences cannot undo a remote send;
// receiver idempotency must include the stable attempt-aware Identity.
func (r AsyncDecisionExecutionRepository) CallbackOutcome(ctx context.Context, lease ExecutionLease, sent bool, message string, next *time.Time) (err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return err
	}
	if sent && (message != "" || next != nil) || !sent && message == "" {
		return ErrExecutionState
	}
	if err = r.scope(r.tenant.String(), message); err != nil {
		return err
	}
	if _, err = optionalDate(next); err != nil {
		return err
	}
	v, err := r.locked(ctx, lease, ports.JobCallback)
	if err != nil {
		return err
	}
	if !terminalExecutionStatus(v.Status) || v.CallbackStatus != "running" || v.CallbackAttemptCount != lease.Identity.DomainAttempt || next != nil && v.CallbackAttemptCount >= v.CallbackMaxAttempts {
		return ErrExecutionState
	}
	status := "failed"
	sentAt := "NULL"
	if sent {
		status = "sent"
		sentAt = "UTC_TIMESTAMP(6)"
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET callback_status=?,callback_last_error=?,callback_sent_at="+sentAt+" WHERE tenant_id=? AND id=?", status, mysqlstore.Text(message), mysqlstore.UUID(r.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
	if err != nil {
		return err
	}
	v, err = r.GetByID(ctx, v.TenantID, v.ID)
	if err != nil {
		return err
	}
	if next != nil {
		if err = r.enqueueAsync(ctx, v, ports.JobCallback, next); err != nil {
			return err
		}
	}
	return r.finishLease(ctx, lease)
}

// Redrive is an explicitly authorized new generation, not a transport retry.
// Compare-and-set prevents stale/reset-all writes. Commit uncertainty requires
// GetByID/receipt reconciliation, not automatic retry with a new generation.
func (r ScheduledExecutionRepository) Redrive(ctx context.Context, id uuid.UUID, expectedGeneration int) (out ScheduledExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRedrive); err != nil {
		return out, err
	}
	if expectedGeneration < 0 || expectedGeneration >= math.MaxInt32 {
		return out, ErrExecutionState
	}
	if err = r.lockResource(ctx, ports.JobScheduled, id); err != nil {
		return out, err
	}
	out, err = r.get(ctx, id)
	if err != nil {
		return out, err
	}
	if out.Status != execution.StatusFailed || out.Generation != expectedGeneration {
		return out, ErrExecutionState
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scheduled_executions SET generation=generation+1,status='pending',attempt_count=0,next_attempt_at=NULL,last_error='',failed_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return out, err
	}
	out, err = r.get(ctx, id)
	if err != nil {
		return out, err
	}
	err = r.enqueueScheduled(ctx, out)
	return out, err
}
func (r AsyncDecisionExecutionRepository) Redrive(ctx context.Context, id uuid.UUID, expectedGeneration int) (out AsyncExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRedrive); err != nil {
		return out, err
	}
	if expectedGeneration < 0 || expectedGeneration >= math.MaxInt32 {
		return out, ErrExecutionState
	}
	if err = r.lockResource(ctx, ports.JobAsync, id); err != nil {
		return out, err
	}
	out, err = r.GetByID(ctx, r.tenant.String(), id.String())
	if err != nil {
		return out, err
	}
	if out.Status != execution.StatusFailed || out.Generation != expectedGeneration {
		return out, ErrExecutionState
	}
	// There can be only the current callback attempt or its one queued successor.
	// Lock/cancel exact identities, never scan/update arbitrary history subsets.
	for _, attempt := range []int{out.CallbackAttemptCount, out.CallbackAttemptCount + 1} {
		if attempt < 1 || attempt > math.MaxInt32 {
			continue
		}
		identity := mysqljobs.Identity{Version: 1, Kind: ports.JobCallback, TenantID: r.tenant, ResourceID: id, DomainAttempt: attempt, RedriveGeneration: out.Generation}
		if err = r.cancelExact(ctx, identity); err != nil {
			return out, err
		}
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET generation=generation+1,status='queued',attempt_count=0,next_attempt_at=NULL,last_error='',result_body=NULL,completed_at=NULL,failed_at=NULL,callback_status='',callback_attempt_count=0,callback_last_error='',callback_sent_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return out, err
	}
	out, err = r.GetByID(ctx, r.tenant.String(), id.String())
	if err != nil {
		return out, err
	}
	err = r.enqueueAsync(ctx, out, ports.JobAsync, nil)
	return out, err
}
func (r executionRepository) cancelExact(ctx context.Context, identity mysqljobs.Identity) error {
	var id mysqlstore.UUID
	err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_execution_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=? FOR UPDATE", mysqlstore.UUID(r.tenant), string(identity.Kind), mysqlstore.UUID(identity.ResourceID), identity.DomainAttempt, identity.RedriveGeneration).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_execution_job_deliveries SET state='cancelled',lease_token=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND kind=? AND id=? AND state IN('available','running')", mysqlstore.UUID(r.tenant), string(identity.Kind), id)
	return err
}
