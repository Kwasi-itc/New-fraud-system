package mysql

import (
	"context"
	"errors"
	"math"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Target-only approved contract; do not expand the PostgreSQL enum or activate
// legacy handlers merely because its typed source Status can hold this string.
const ExecutionStatusCancelled execution.Status = "cancelled"

func terminalExecutionStatus(status execution.Status) bool {
	return status == execution.StatusCompleted || status == execution.StatusFailed || status == ExecutionStatusCancelled
}

func cancellationAttempt(status, pending execution.Status, count, maximum int) (int, error) {
	if count < 0 || maximum < 1 || maximum > math.MaxInt32 || count > maximum {
		return 0, ErrExecutionState
	}
	if status == execution.StatusRunning && count > 0 {
		return count, nil
	}
	if status == pending && count < maximum {
		return count + 1, nil
	}
	return 0, ErrExecutionState
}

// Cancelling one active owner must cancel its ACTUAL required delivery; missing
// transport is corruption, not success. Permanent receipt -> delivery is the
// same lock order used by finish/rescue. Owner lock is already held.
func (r executionRepository) cancelCurrent(ctx context.Context, identity mysqljobs.Identity) error {
	if identity.Validate() != nil || identity.TenantID != r.tenant || executionOwner(identity.Kind) == "" {
		return ErrExecutionState
	}
	var id mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_execution_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=? FOR UPDATE", mysqlstore.UUID(r.tenant), string(identity.Kind), mysqlstore.UUID(identity.ResourceID), identity.DomainAttempt, identity.RedriveGeneration).Scan(&id); err != nil {
		return err
	}
	result, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_execution_job_deliveries SET state='cancelled',lease_token=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),last_error_code='owner_cancelled' WHERE tenant_id=? AND kind=? AND id=? AND state IN('available','running')", mysqlstore.UUID(r.tenant), string(identity.Kind), id)
	return executionChanged(result, err)
}

// Cancel affects ONE occurrence, not its recurring definition. It keeps the
// generation; owner status plus invalidated transport fences stale workers.
// Same-generation repeat cancellation is idempotent, never a new callback/job.
// Redrive is a separate action; cancelled evaluation redrive is not enabled.
func (r ScheduledExecutionRepository) Cancel(ctx context.Context, id uuid.UUID, expectedGeneration int) (out ScheduledExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionCancel); err != nil {
		return out, err
	}
	if expectedGeneration < 0 || expectedGeneration > math.MaxInt32 {
		return out, ErrExecutionState
	}
	if err = r.lockResource(ctx, ports.JobScheduled, id); err != nil {
		return out, err
	}
	out, err = r.get(ctx, id)
	if err != nil {
		return out, err
	}
	if out.Generation != expectedGeneration {
		return out, ErrExecutionState
	}
	if out.Status == ExecutionStatusCancelled {
		return out, nil
	}
	attempt, err := cancellationAttempt(out.Status, execution.StatusPending, out.AttemptCount, out.MaxAttempts)
	if err != nil {
		return out, err
	}
	identity := mysqljobs.Identity{Version: 1, Kind: ports.JobScheduled, TenantID: r.tenant, ResourceID: id, DomainAttempt: attempt, RedriveGeneration: out.Generation}
	if err = r.cancelCurrent(ctx, identity); err != nil {
		return out, err
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scheduled_executions SET status='cancelled',cancelled_at=UTC_TIMESTAMP(6),failed_at=NULL,next_attempt_at=NULL,last_error='execution_cancelled' WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return out, err
	}
	return r.get(ctx, id)
}

var errCallbackAlreadySent = errors.New("callback already sent; cancellation cannot undo external delivery")

func (r AsyncDecisionExecutionRepository) Cancel(ctx context.Context, id uuid.UUID, expectedGeneration int) (out AsyncExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionCancel); err != nil {
		return out, err
	}
	if expectedGeneration < 0 || expectedGeneration > math.MaxInt32 {
		return out, ErrExecutionState
	}
	if err = r.lockResource(ctx, ports.JobAsync, id); err != nil {
		return out, err
	}
	out, err = r.GetByID(ctx, r.tenant.String(), id.String())
	if err != nil {
		return out, err
	}
	if out.Generation != expectedGeneration {
		return out, ErrExecutionState
	}
	if out.Status == ExecutionStatusCancelled {
		return out, nil
	}
	if out.CallbackStatus != "" || out.CallbackAttemptCount != 0 {
		return out, ErrExecutionState
	}
	attempt, err := cancellationAttempt(out.Status, execution.StatusQueued, out.AttemptCount, out.MaxAttempts)
	if err != nil {
		return out, err
	}
	identity := mysqljobs.Identity{Version: 1, Kind: ports.JobAsync, TenantID: r.tenant, ResourceID: id, DomainAttempt: attempt, RedriveGeneration: out.Generation}
	if err = r.cancelCurrent(ctx, identity); err != nil {
		return out, err
	}
	callback := ""
	if out.CallbackURL != "" {
		callback = "pending"
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET status='cancelled',cancelled_at=UTC_TIMESTAMP(6),failed_at=NULL,completed_at=NULL,next_attempt_at=NULL,result_body=NULL,last_error='execution_cancelled',callback_status=?,callback_last_error='',callback_cancelled_at=NULL WHERE tenant_id=? AND id=?", callback, mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return out, err
	}
	out, err = r.GetByID(ctx, r.tenant.String(), id.String())
	if err != nil {
		return out, err
	}
	if callback != "" {
		err = r.enqueueAsync(ctx, out, ports.JobCallback, nil)
	}
	return out, err
}

// CancelCallback preserves the evaluation/result/timestamps. It stops future
// local delivery/acknowledgement, not a remote send already in flight. A failed
// callback may have a queued successor or none; inspect only its two exact
// identities, never an arbitrary history scan. Pending/running require the
// actual active job; an already-sent callback is never relabelled cancelled.
func (r AsyncDecisionExecutionRepository) CancelCallback(ctx context.Context, id uuid.UUID, expectedGeneration int) (out AsyncExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionCancel); err != nil {
		return out, err
	}
	if expectedGeneration < 0 || expectedGeneration > math.MaxInt32 {
		return out, ErrExecutionState
	}
	if err = r.lockResource(ctx, ports.JobCallback, id); err != nil {
		return out, err
	}
	out, err = r.GetByID(ctx, r.tenant.String(), id.String())
	if err != nil {
		return out, err
	}
	if out.Generation != expectedGeneration || !terminalExecutionStatus(out.Status) || out.CallbackURL == "" {
		return out, ErrExecutionState
	}
	if out.CallbackStatus == "cancelled" {
		return out, nil
	}
	if out.CallbackStatus == "sent" {
		return out, errCallbackAlreadySent
	}
	identity := mysqljobs.Identity{Version: 1, Kind: ports.JobCallback, TenantID: r.tenant, ResourceID: id, RedriveGeneration: out.Generation}
	switch out.CallbackStatus {
	case "pending", "running":
		pending := execution.Status("pending")
		attempt, e := cancellationAttempt(execution.Status(out.CallbackStatus), pending, out.CallbackAttemptCount, out.CallbackMaxAttempts)
		if e != nil {
			return out, e
		}
		identity.DomainAttempt = attempt
		if err = r.cancelCurrent(ctx, identity); err != nil {
			return out, err
		}
	case "failed":
		for _, attempt := range []int{out.CallbackAttemptCount, out.CallbackAttemptCount + 1} {
			if attempt < 1 || attempt > out.CallbackMaxAttempts {
				continue
			}
			identity.DomainAttempt = attempt
			if err = r.cancelExact(ctx, identity); err != nil {
				return out, err
			}
		}
	default:
		return out, ErrExecutionState
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET callback_status='cancelled',callback_cancelled_at=UTC_TIMESTAMP(6),callback_last_error='callback_cancelled',callback_sent_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return out, err
	}
	return r.GetByID(ctx, r.tenant.String(), id.String())
}
