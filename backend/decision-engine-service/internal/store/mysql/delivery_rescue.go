package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// Owner row must already be locked. Discovery never grants authority and never
// lends an expired worker a temporary live lease. Global lock order remains
// resource -> receipt -> delivery, also for rescue versus finish/cancellation.
func (s *EffectDeliveryStore) lockExpired(ctx context.Context, tx *sql.Tx, v ExpiredDelivery) (maximum int, errorsBefore int64, err error) {
	if !s.validExpired(v) {
		return 0, 0, ErrStaleEffectLease
	}
	var receipt mysqlstore.UUID
	err = tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+"."+s.receiptTable()+" WHERE tenant_id=? AND kind=? AND id=? AND resource_id=? AND domain_attempt=? AND redrive_generation=? FOR SHARE", mysqlstore.UUID(s.tenant), string(s.kind), mysqlstore.UUID(v.ID), mysqlstore.UUID(v.Identity.ResourceID), v.Identity.DomainAttempt, v.Identity.RedriveGeneration).Scan(&receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrStaleEffectLease
	}
	if err != nil {
		return 0, 0, err
	}
	err = tx.QueryRowContext(ctx, "SELECT max_attempts,error_count FROM "+s.schema+"."+s.deliveryTable()+expiredDeliveryPredicate+" FOR UPDATE", s.expiredArgs(v)...).Scan(&maximum, &errorsBefore)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrStaleEffectLease
	}
	return maximum, errorsBefore, err
}

func (s *EffectDeliveryStore) finishExpired(ctx context.Context, tx *sql.Tx, v ExpiredDelivery, state workers.DeliveryState, delay time.Duration, priorErrors int64) error {
	finished, code := "NULL", "lease_expired"
	if state == workers.Discarded {
		finished, code = "UTC_TIMESTAMP(6)", "delivery_attempts_exhausted"
	} else if state != workers.Available {
		return errors.New("rescue must requeue or discard, never report success")
	}
	args := []any{string(state), delay.Microseconds(), code}
	args = append(args, s.expiredArgs(v)...)
	args = append(args, priorErrors)
	result, err := tx.ExecContext(ctx, "UPDATE "+s.schema+"."+s.deliveryTable()+" SET state=?,available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),last_error_code=?,error_count=error_count+1,lease_token=NULL,lease_until=NULL,finished_at="+finished+expiredDeliveryPredicate+" AND error_count=? AND error_count<attempt", args...)
	return effectDeliveryChanged(result, err)
}
