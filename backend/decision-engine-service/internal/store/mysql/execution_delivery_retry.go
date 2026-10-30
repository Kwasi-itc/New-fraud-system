package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

// RetryDelivery records a TRANSPORT failure, not a new domain attempt. A running
// owner remains on its current attempt when requeued. Exhaustion atomically
// records terminal owner failure (and its required callback) with discard. A job
// exhausted before Start may fail with zero consumed domain attempts; transport
// evidence remains on its receipt/delivery. No reservation receipt is released.
func (r ScheduledExecutionRepository) RetryDelivery(ctx context.Context, lease ExecutionLease, errorCode string) (state workers.DeliveryState, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return state, err
	}
	v, err := r.locked(ctx, lease)
	if err != nil {
		return state, err
	}
	if err = validateScheduledDelivery(v, lease.Identity); err != nil {
		return state, err
	}
	return r.retryDelivery(ctx, lease, errorCode, func() error { return r.exhaustDelivery(ctx, v) })
}

func validateScheduledDelivery(v ScheduledExecutionRecord, identity mysqljobs.Identity) error {
	if identity.Validate() != nil || identity.Kind != ports.JobScheduled || identity.ResourceID.String() != v.ID || identity.TenantID.String() != v.TenantID {
		return ErrExecutionState
	}
	if v.Generation != identity.RedriveGeneration || identity.DomainAttempt > v.MaxAttempts || !evaluationDeliveryEligible(v.Status, execution.StatusPending, v.AttemptCount, identity.DomainAttempt) {
		return ErrExecutionState
	}
	return nil
}

func (r ScheduledExecutionRepository) exhaustDelivery(ctx context.Context, v ScheduledExecutionRecord) error {
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scheduled_executions SET status='failed',next_attempt_at=NULL,failed_at=UTC_TIMESTAMP(6),last_error='delivery_attempts_exhausted' WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(uuid.MustParse(v.ID)))
	return err
}

func evaluationDeliveryEligible(status, pending execution.Status, count, expected int) bool {
	return status == execution.StatusRunning && count == expected || status == pending && count < math.MaxInt32 && count+1 == expected
}

func (r AsyncDecisionExecutionRepository) RetryDelivery(ctx context.Context, lease ExecutionLease, errorCode string) (state workers.DeliveryState, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRun); err != nil {
		return state, err
	}
	kind := lease.Identity.Kind
	if kind != ports.JobAsync && kind != ports.JobCallback {
		return state, ErrExecutionLease
	}
	v, err := r.locked(ctx, lease, kind)
	if err != nil {
		return state, err
	}
	if err = validateAsyncDelivery(v, lease.Identity); err != nil {
		return state, err
	}
	return r.retryDelivery(ctx, lease, errorCode, func() error { return r.exhaustDelivery(ctx, v, kind) })
}

func validateAsyncDelivery(v AsyncExecutionRecord, identity mysqljobs.Identity) error {
	if identity.Validate() != nil || identity.ResourceID.String() != v.ID || identity.TenantID.String() != v.TenantID {
		return ErrExecutionState
	}
	if v.Generation != identity.RedriveGeneration {
		return ErrExecutionState
	}
	if identity.Kind == ports.JobAsync {
		if identity.DomainAttempt > v.MaxAttempts || !evaluationDeliveryEligible(v.Status, execution.StatusQueued, v.AttemptCount, identity.DomainAttempt) {
			return ErrExecutionState
		}
	} else if identity.Kind == ports.JobCallback {
		if !terminalExecutionStatus(v.Status) || v.CallbackURL == "" || identity.DomainAttempt > v.CallbackMaxAttempts {
			return ErrExecutionState
		}
		eligible := v.CallbackStatus == "running" && v.CallbackAttemptCount == identity.DomainAttempt || (v.CallbackStatus == "pending" || v.CallbackStatus == "failed") && v.CallbackAttemptCount < math.MaxInt32 && v.CallbackAttemptCount+1 == identity.DomainAttempt
		if !eligible {
			return ErrExecutionState
		}
	} else {
		return ErrExecutionState
	}
	return nil
}

func (r AsyncDecisionExecutionRepository) exhaustDelivery(ctx context.Context, v AsyncExecutionRecord, kind ports.JobKind) error {
	if kind == ports.JobCallback {
		_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET callback_status='failed',callback_last_error='delivery_attempts_exhausted',callback_sent_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(uuid.MustParse(v.ID)))
		return err
	}
	callback := ""
	if v.CallbackURL != "" {
		callback = "pending"
	}
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET status='failed',next_attempt_at=NULL,failed_at=UTC_TIMESTAMP(6),completed_at=NULL,result_body=NULL,last_error='delivery_attempts_exhausted',callback_status=? WHERE tenant_id=? AND id=?", callback, mysqlstore.UUID(r.tenant), mysqlstore.UUID(uuid.MustParse(v.ID)))
	if err != nil {
		return err
	}
	if callback != "" {
		current, err := r.GetByID(ctx, r.tenant.String(), v.ID)
		if err != nil {
			return err
		}
		return r.enqueueAsync(ctx, current, ports.JobCallback, nil)
	}
	return nil
}

func (r executionRepository) retryDelivery(ctx context.Context, lease ExecutionLease, errorCode string, exhausted func() error) (workers.DeliveryState, error) {
	if !effectErrorCode.MatchString(errorCode) || exhausted == nil {
		return "", errors.New("redacted transport error code and local exhaustion outcome required")
	}
	var maximum int
	var priorErrors int64
	err := r.tx.QueryRowContext(ctx, "SELECT max_attempts,error_count FROM "+r.schema+".decision_engine_execution_job_deliveries"+executionLeasePredicate+" FOR UPDATE", r.leaseArgs(lease)...).Scan(&maximum, &priorErrors)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrExecutionLease
	}
	if err != nil {
		return "", err
	}
	state, delay, err := deliveryFailurePlan(lease.TransportAttempt, maximum, priorErrors)
	if err != nil {
		return "", err
	}
	finished := "NULL"
	if state == workers.Discarded {
		if err := exhausted(); err != nil {
			return "", err
		}
		errorCode, finished = "delivery_attempts_exhausted", "UTC_TIMESTAMP(6)"
	}
	args := []any{string(state), delay.Microseconds(), errorCode}
	args = append(args, r.leaseArgs(lease)...)
	result, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_execution_job_deliveries SET state=?,available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),last_error_code=?,error_count=error_count+1,lease_token=NULL,lease_until=NULL,finished_at="+finished+executionLeasePredicate, args...)
	if err := executionChanged(result, err); err != nil {
		return "", err
	}
	return state, nil
}

// RedriveCallback is explicitly authorized and generation-conditional. It
// resends a terminal evaluation's callback without rerunning the evaluation or
// clearing its result, timestamps or domain counter. Old sends cannot be undone.
func (r AsyncDecisionExecutionRepository) RedriveCallback(ctx context.Context, id uuid.UUID, expectedGeneration int) (out AsyncExecutionRecord, err error) {
	defer r.poison(&err)
	if err = r.require(ExecutionRedrive); err != nil {
		return out, err
	}
	if expectedGeneration < 0 || expectedGeneration >= math.MaxInt32 {
		return out, ErrExecutionState
	}
	if err = r.lockResource(ctx, ports.JobCallback, id); err != nil {
		return out, err
	}
	out, err = r.GetByID(ctx, r.tenant.String(), id.String())
	if err != nil {
		return out, err
	}
	if out.Generation != expectedGeneration || !terminalExecutionStatus(out.Status) || out.CallbackURL == "" || (out.CallbackStatus != "failed" && out.CallbackStatus != "cancelled") {
		return out, ErrExecutionState
	}
	for _, attempt := range []int{out.CallbackAttemptCount, out.CallbackAttemptCount + 1} {
		if attempt < 1 || attempt > math.MaxInt32 {
			continue
		}
		identity := mysqljobs.Identity{Version: 1, Kind: ports.JobCallback, TenantID: r.tenant, ResourceID: id, DomainAttempt: attempt, RedriveGeneration: out.Generation}
		if err = r.cancelExact(ctx, identity); err != nil {
			return out, err
		}
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_async_executions SET generation=generation+1,callback_status='pending',callback_attempt_count=0,callback_last_error='',callback_sent_at=NULL,callback_cancelled_at=NULL WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id))
	if err != nil {
		return out, err
	}
	out, err = r.GetByID(ctx, r.tenant.String(), id.String())
	if err != nil {
		return out, err
	}
	err = r.enqueueAsync(ctx, out, ports.JobCallback, nil)
	return out, err
}
