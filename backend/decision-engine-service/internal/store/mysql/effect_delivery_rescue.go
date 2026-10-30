package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// EffectExhaustionHandler is the trusted domain owner's LOCAL SQL mutation,
// required even if the observed attempt currently looks retryable. It must
// validate/update the real failed outcome (and any required successor jobs) in
// the provided transaction. No network I/O, commit, queue writes or retained tx.
// A generic success/no-op implementation is not a production provider. Real
// workflow/screening/scoring/outbox handler composition remains activation work.
type EffectExhaustionHandler func(context.Context, *sql.Tx, mysqljobs.Identity) error

// Rescue locks one resource -> receipt -> exact expired delivery. It never
// presents an expired lease to Finish or silently marks business work complete.
// An exhaustion-handler error rolls back both the owner and transport outcome.
// Receipts are deliberately retained, including after transport exhaustion.
func (s *EffectDeliveryStore) Rescue(ctx context.Context, v ExpiredDelivery, exhausted EffectExhaustionHandler) (workers.DeliveryState, error) {
	if s == nil || s.db == nil || s.authorize == nil || s.executionKinds || exhausted == nil {
		return "", errors.New("scoped effect transport and real local exhaustion handler required")
	}
	if !s.validExpired(v) {
		return "", ErrStaleEffectLease
	}
	var state workers.DeliveryState
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, EffectWorkerRescue); err != nil {
			return err
		}
		var resource mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+"."+effectOwnerTable(s.kind)+" WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.Identity.ResourceID)).Scan(&resource); err != nil {
			return err
		}
		if err := s.fenceCurrentEffect(ctx, tx, v.Identity, v.ID); err != nil {
			return err
		}
		maximum, priorErrors, err := s.lockExpired(ctx, tx, v)
		if err != nil {
			return err
		}
		outcome, delay, err := deliveryFailurePlan(v.TransportAttempt, maximum, priorErrors)
		if err != nil {
			return err
		}
		if outcome == workers.Discarded {
			if err := s.checkDispatchExhaustion(ctx, tx, v); err != nil {
				return err
			}
			if err := exhausted(ctx, tx, v.Identity); err != nil {
				return err
			}
		}
		if err := s.finishExpired(ctx, tx, v, outcome, delay, priorErrors); err != nil {
			return err
		}
		state = outcome
		return nil
	})
	if err != nil {
		return "", err
	}
	return state, nil
}
