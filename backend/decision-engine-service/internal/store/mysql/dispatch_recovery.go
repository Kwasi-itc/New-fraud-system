package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// ClaimReconciliation renews an expired, exhausted transport only when a
// retained send intent needs reconciliation. Attempt/domain/generation and the
// original payload remain unchanged. The new token fences the previous worker;
// all network IO happens after this transaction commits.
func (s *DispatchStore) ClaimReconciliation(ctx context.Context) (out *EffectDeliveryLease, err error) {
	if s == nil || s.delivery == nil {
		return nil, ErrOwnerContractUnavailable
	}
	d := s.delivery
	var candidate ExpiredDelivery
	candidate.Identity = mysqljobs.Identity{Version: 1, TenantID: d.tenant, Kind: d.kind}
	err = mysqlstore.Snapshot(ctx, d.db, func(tx *sql.Tx) error {
		if err := d.authorize(ctx, tx, d.tenant, d.kind, EffectWorkerRescue); err != nil {
			return err
		}
		query, args := d.expiredSelectFiltered(nil, 1, true)
		var until mysqlstore.DateTime
		err := tx.QueryRowContext(ctx, query, args...).Scan((*mysqlstore.UUID)(&candidate.ID), (*mysqlstore.UUID)(&candidate.Token), &candidate.TransportAttempt, &until, (*mysqlstore.UUID)(&candidate.Identity.ResourceID), &candidate.Identity.DomainAttempt, &candidate.Identity.RedriveGeneration)
		candidate.LeaseUntil = until.Time
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = mysqlstore.Mutation(ctx, d.db, func(tx *sql.Tx) error {
		if err := d.authorize(ctx, tx, d.tenant, d.kind, EffectWorkerRescue); err != nil {
			return err
		}
		table := effectOwnerTable(d.kind)
		if s.executions != nil {
			if err := s.executions.authorize(ctx, tx, d.tenant, ExecutionRescue); err != nil {
				return err
			}
			table = executionOwner(d.kind)
		} else if err := s.effects.configuration.authorize(ctx, tx, d.tenant, true); err != nil {
			return err
		}
		var resource mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+d.schema+"."+table+" WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(d.tenant), mysqlstore.UUID(candidate.Identity.ResourceID)).Scan(&resource); err != nil {
			return err
		}
		if !d.executionKinds {
			if err := d.fenceCurrentEffect(ctx, tx, candidate.Identity, candidate.ID); err != nil {
				return err
			}
		}
		maximum, _, err := d.lockExpired(ctx, tx, candidate)
		if err != nil {
			return err
		}
		if candidate.TransportAttempt < maximum {
			return ErrStaleEffectLease
		}
		var receipt mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT receipt_id FROM "+d.schema+".decision_engine_dispatch_intents WHERE tenant_id=? AND receipt_id=? FOR UPDATE", mysqlstore.UUID(d.tenant), mysqlstore.UUID(candidate.ID)).Scan(&receipt); err != nil {
			return err
		}
		lease := EffectDeliveryLease{ID: candidate.ID, Token: uuid.New(), Identity: candidate.Identity, Attempt: candidate.TransportAttempt}
		args := append([]any{mysqlstore.UUID(lease.Token), d.lease.Microseconds()}, d.expiredArgs(candidate)...)
		if err := effectDeliveryChanged(tx.ExecContext(ctx, "UPDATE "+d.schema+"."+d.deliveryTable()+" SET lease_token=?,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))"+expiredDeliveryPredicate, args...)); err != nil {
			return err
		}
		out = &lease
		return nil
	})
	if err == ErrStaleEffectLease || err == ErrExecutionLease {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
