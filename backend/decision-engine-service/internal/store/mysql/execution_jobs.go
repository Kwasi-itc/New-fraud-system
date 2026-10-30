package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

type executionJob struct {
	identity mysqljobs.Identity
	at       *time.Time
}

func (r executionRepository) enqueueScheduled(ctx context.Context, v ScheduledExecutionRecord) error {
	if v.Status != execution.StatusPending || v.AttemptCount >= v.MaxAttempts || v.AttemptCount == math.MaxInt32 {
		return ErrExecutionState
	}
	at := v.ScheduledFor
	if v.NextAttemptAt != nil && v.NextAttemptAt.After(at) {
		at = *v.NextAttemptAt
	}
	return r.enqueueExecutionJobs(ctx, []executionJob{{mysqljobs.Identity{Version: 1, Kind: ports.JobScheduled, TenantID: r.tenant, ResourceID: uuid.MustParse(v.ID), DomainAttempt: v.AttemptCount + 1, RedriveGeneration: v.Generation}, &at}})
}
func (r executionRepository) enqueueAsync(ctx context.Context, v AsyncExecutionRecord, kind ports.JobKind, at *time.Time) error {
	attempt := v.AttemptCount
	if kind == ports.JobAsync {
		if v.Status != execution.StatusQueued || attempt >= v.MaxAttempts {
			return ErrExecutionState
		}
	} else if kind == ports.JobCallback {
		attempt = v.CallbackAttemptCount
		if v.CallbackURL == "" || !terminalExecutionStatus(v.Status) || (v.CallbackStatus != "pending" && v.CallbackStatus != "failed") || attempt >= v.CallbackMaxAttempts {
			return ErrExecutionState
		}
	} else {
		return errors.New("unsupported execution job kind")
	}
	if attempt == math.MaxInt32 {
		return ErrExecutionState
	}
	return r.enqueueExecutionJobs(ctx, []executionJob{{mysqljobs.Identity{Version: 1, Kind: kind, TenantID: r.tenant, ResourceID: uuid.MustParse(v.ID), DomainAttempt: attempt + 1, RedriveGeneration: v.Generation}, at}})
}

// Private owner producer: callers have just inserted or locked/updated these
// resources on this transaction. No public arbitrary-identity enqueue exists.
// New domain attempts get new exact receipts; transport retries keep their row.
func (r executionRepository) enqueueExecutionJobs(ctx context.Context, jobs []executionJob) error {
	if len(jobs) > r.maxRows {
		return ErrProjectionLimit
	}
	seen := map[mysqljobs.ReservationKey]bool{}
	for _, job := range jobs {
		key, err := job.identity.Key()
		if err != nil {
			return err
		}
		if job.identity.TenantID != r.tenant || executionOwner(job.identity.Kind) == "" || seen[key] {
			return ErrExecutionReplayConflict
		}
		seen[key] = true
		if _, ok := r.policy.Queues[job.identity.Kind]; !ok {
			return errors.New("required execution producer unavailable")
		}
		if _, err := optionalDate(job.at); err != nil {
			return err
		}
	}
	for start := 0; start < len(jobs); start += 100 {
		batch := jobs[start:min(start+100, len(jobs))]
		var receiptMarks, deliveryMarks []string
		var receiptArgs, deliveryArgs []any
		for _, job := range batch {
			v := job.identity
			id := mysqlstore.UUID(uuid.New())
			var scheduled, async mysqlstore.NullUUID
			if v.Kind == ports.JobScheduled {
				scheduled = mysqlstore.NullUUID{UUID: mysqlstore.UUID(v.ResourceID), Valid: true}
			} else {
				async = mysqlstore.NullUUID{UUID: mysqlstore.UUID(v.ResourceID), Valid: true}
			}
			receiptMarks = append(receiptMarks, "(?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))")
			receiptArgs = append(receiptArgs, id, mysqlstore.UUID(r.tenant), string(v.Kind), mysqlstore.UUID(v.ResourceID), v.DomainAttempt, v.RedriveGeneration, scheduled, async)
			p := r.policy.Queues[v.Kind]
			at, _ := optionalDate(job.at)
			deliveryMarks = append(deliveryMarks, "(?,?,?,?,GREATEST(UTC_TIMESTAMP(6),COALESCE(?,UTC_TIMESTAMP(6))),UTC_TIMESTAMP(6),?)")
			deliveryArgs = append(deliveryArgs, id, mysqlstore.UUID(r.tenant), string(v.Kind), p.Name, at, p.MaxAttempts)
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_execution_job_receipts(id,tenant_id,kind,resource_id,domain_attempt,redrive_generation,scheduled_id,async_id,created_at) VALUES"+strings.Join(receiptMarks, ","), receiptArgs...); err != nil {
			return err
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_execution_job_deliveries(id,tenant_id,kind,queue_name,available_at,created_at,max_attempts) VALUES"+strings.Join(deliveryMarks, ","), deliveryArgs...); err != nil {
			return err
		}
	}
	return nil
}

func (r executionRepository) validLease(v ExecutionLease, kind ports.JobKind) bool {
	return v.ID != uuid.Nil && v.Token != uuid.Nil && v.TransportAttempt > 0 && v.TransportAttempt <= math.MaxInt32 && v.Identity.Validate() == nil && v.Identity.TenantID == r.tenant && v.Identity.Kind == kind && executionOwner(kind) != ""
}

const executionLeasePredicate = " WHERE tenant_id=? AND kind=? AND queue_name=? AND id=? AND state='running' AND lease_token=? AND attempt=? AND lease_until>UTC_TIMESTAMP(6)"

func (r executionRepository) leaseArgs(v ExecutionLease) []any {
	return []any{mysqlstore.UUID(r.tenant), string(v.Identity.Kind), r.policy.Queues[v.Identity.Kind].Name, mysqlstore.UUID(v.ID), mysqlstore.UUID(v.Token), v.TransportAttempt}
}

// Owner row must already be locked. Receipt -> delivery is the global order;
// transport claim/heartbeat never acquire owner rows while holding a queue row.
func (r executionRepository) lockLease(ctx context.Context, v ExecutionLease, kind ports.JobKind) error {
	if !r.validLease(v, kind) {
		return ErrExecutionLease
	}
	var id mysqlstore.UUID
	err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_execution_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=? AND id=? FOR SHARE", mysqlstore.UUID(r.tenant), string(kind), mysqlstore.UUID(v.Identity.ResourceID), v.Identity.DomainAttempt, v.Identity.RedriveGeneration, mysqlstore.UUID(v.ID)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExecutionLease
	}
	if err != nil {
		return err
	}
	var count int64
	err = r.tx.QueryRowContext(ctx, "SELECT error_count FROM "+r.schema+".decision_engine_execution_job_deliveries"+executionLeasePredicate+" FOR UPDATE", r.leaseArgs(v)...).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExecutionLease
	}
	if err != nil {
		return err
	}
	return workers.ValidateErrorHistory(v.TransportAttempt, &count, false)
}
func executionChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrExecutionLease
	}
	return nil
}
func (r executionRepository) finishLease(ctx context.Context, v ExecutionLease) error {
	result, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_execution_job_deliveries SET state='completed',finished_at=UTC_TIMESTAMP(6),lease_token=NULL,lease_until=NULL"+executionLeasePredicate, r.leaseArgs(v)...)
	return executionChanged(result, err)
}

func (r executionRepository) lockResource(ctx context.Context, kind ports.JobKind, id uuid.UUID) error {
	if id == uuid.Nil || executionOwner(kind) == "" {
		return ErrTenantScope
	}
	var locked mysqlstore.UUID
	return r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+"."+executionOwner(kind)+" WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)).Scan(&locked)
}
