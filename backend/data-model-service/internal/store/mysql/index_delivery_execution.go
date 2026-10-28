package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// LeasedIndexJobExecutionStore binds the existing worker metadata port to one
// committed delivery lease. It does not implement a polling loop or physical DDL
// fencing. DDL still requires the pinned execution lock and readiness protocol.
type LeasedIndexJobExecutionStore struct {
	delivery   *IndexDeliveryStore
	lease      DeliveryLease
	maxBytes   int
	retryDelay time.Duration
}

func NewLeasedIndexJobExecutionStore(delivery *IndexDeliveryStore, lease DeliveryLease, maxBytes int, retryDelay time.Duration) (*LeasedIndexJobExecutionStore, error) {
	if delivery == nil || !validLease(lease) || maxBytes < 1 || maxBytes > 64<<20 || retryDelay < 0 || retryDelay > 24*time.Hour || retryDelay%time.Microsecond != 0 {
		return nil, errors.New("delivery store, committed lease, byte limit and retry delay required")
	}
	if delivery.retryPolicy != nil && retryDelay != 0 {
		return nil, errors.New("policy-controlled delivery requires zero fixed retry delay")
	}
	return &LeasedIndexJobExecutionStore{delivery, lease, maxBytes, retryDelay}, nil
}

func (s *LeasedIndexJobExecutionStore) checkLease(ctx context.Context, tx *sql.Tx) error {
	v := s.lease
	var found mysqlstore.UUID
	err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.delivery.schema+".data_model_index_deliveries WHERE tenant_id=? AND job_id=? AND id=? AND queue_name=? AND state='running' AND lease_token=? AND attempt=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.JobID), mysqlstore.UUID(v.ID), s.delivery.queue, mysqlstore.UUID(v.Token), v.Attempt).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStaleDeliveryLease
	}
	return err
}

func terminalIndexJob(status datamodel.IndexJobStatus) bool {
	return status == datamodel.IndexJobStatusApplied || status == datamodel.IndexJobStatusFailed || status == datamodel.IndexJobStatusCancelled
}

func (s *LeasedIndexJobExecutionStore) StartAttempt(ctx context.Context, id uuid.UUID, at time.Time) (datamodel.IndexJob, error) {
	if id != s.lease.JobID {
		return datamodel.IndexJob{}, ErrStaleDeliveryLease
	}
	if _, err := (mysqlstore.DateTime{Time: at}).Value(); err != nil {
		return datamodel.IndexJob{}, err
	}
	var job datamodel.IndexJob
	err := mysqlstore.Mutation(ctx, s.delivery.db, func(tx *sql.Tx) error {
		if err := s.delivery.lockJob(ctx, tx, s.lease.TenantID, id, false); err != nil {
			return err
		}
		if err := s.checkLease(ctx, tx); err != nil {
			return err
		}
		r := IndexJobRepository{metadataRepository{tx, s.delivery.schema, s.lease.TenantID, 100000, s.maxBytes}}
		var err error
		job, err = r.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if !terminalIndexJob(job.Status) {
			job, err = r.StartAttempt(ctx, id, at)
			if err != nil {
				return err
			}
		}
		return s.checkLease(ctx, tx)
	})
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	if terminalIndexJob(job.Status) {
		// Runner returns immediately for terminal redelivery. Acknowledge it only
		// after rechecking the job under the queue's job-before-delivery lock order.
		err = s.delivery.Finish(ctx, s.lease, false, 0, "", func(tx *sql.Tx) error {
			r := IndexJobRepository{metadataRepository{tx, s.delivery.schema, s.lease.TenantID, 100000, s.maxBytes}}
			current, err := r.GetByID(ctx, id)
			if err != nil {
				return err
			}
			if !terminalIndexJob(current.Status) {
				return ports.ErrStaleIndexJobAttempt
			}
			job = current
			return nil
		})
		if err != nil {
			return datamodel.IndexJob{}, err
		}
	}
	return job, nil
}

func (s *LeasedIndexJobExecutionStore) CompleteAttempt(ctx context.Context, o ports.IndexJobOutcome) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if o.Job.ID != s.lease.JobID || o.Job.TenantID != s.lease.TenantID {
		return ErrStaleDeliveryLease
	}
	if len(o.Change.Details)+len(o.ErrorMessage) > s.maxBytes {
		return ErrSnapshotLimit
	}
	retry := o.Change.Status == string(datamodel.IndexJobStatusPending) || o.RetryDelivery
	code := ""
	if retry {
		code = "index_attempt_retry"
	}
	return s.delivery.Finish(ctx, s.lease, retry, s.retryDelay, code, func(tx *sql.Tx) error {
		return completeIndexAttemptTx(ctx, tx, s.delivery.schema, o)
	})
}

var _ ports.IndexJobExecutionStore = (*LeasedIndexJobExecutionStore)(nil)
