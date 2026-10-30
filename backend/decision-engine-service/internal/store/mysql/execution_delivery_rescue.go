package mysql

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// Rescue requires BOTH the explicit owner-rescue authority and scoped worker
// rescue authority, in one caller transaction before locking. The exact same
// pool/schema/tenant/queue must be configured on both adapters. There is no
// poller/handler activation here, and a candidate is not a work lease.
func (s *ExecutionDeliveryStore) Rescue(ctx context.Context, owner *ExecutionStore, candidate ExpiredDelivery) (workers.DeliveryState, error) {
	if s == nil || s.transport == nil || owner == nil || owner.configuration == nil || owner.authorize == nil {
		return "", errors.New("initialized execution transport and owner required")
	}
	t, c := s.transport, owner.configuration
	if !t.executionKinds || t.db != c.db || t.schema != c.schema || t.tenant != c.tenant || owner.policy.Queues[t.kind].Name != t.queue || !t.validExpired(candidate) {
		return "", ErrExecutionLease
	}
	var outcome workers.DeliveryState
	err := owner.Run(ctx, ExecutionRescue, func(repos ExecutionRepositories) error {
		r := repos.Scheduled.executionRepository
		if err := t.authorize(ctx, r.tx, t.tenant, t.kind, ExecutionWorkerRescue); err != nil {
			return err
		}
		if err := r.lockResource(ctx, t.kind, candidate.Identity.ResourceID); err != nil {
			return err
		}
		// Fence the observed delivery before interpreting current owner state.
		// A concurrent cancellation/completion may have legitimately changed
		// both; that is a stale candidate, not a broken owner-state invariant.
		maximum, priorErrors, err := t.lockExpired(ctx, r.tx, candidate)
		if err != nil {
			return err
		}
		var exhausted func() error
		switch t.kind {
		case ports.JobScheduled:
			v, err := repos.Scheduled.get(ctx, candidate.Identity.ResourceID)
			if err != nil {
				return err
			}
			if err := validateScheduledDelivery(v, candidate.Identity); err != nil {
				return err
			}
			exhausted = func() error { return repos.Scheduled.exhaustDelivery(ctx, v) }
		case ports.JobAsync, ports.JobCallback:
			v, err := repos.Async.GetByID(ctx, t.tenant.String(), candidate.Identity.ResourceID.String())
			if err != nil {
				return err
			}
			if err := validateAsyncDelivery(v, candidate.Identity); err != nil {
				return err
			}
			exhausted = func() error { return repos.Async.exhaustDelivery(ctx, v, t.kind) }
		default:
			return ErrExecutionLease
		}
		state, delay, err := deliveryFailurePlan(candidate.TransportAttempt, maximum, priorErrors)
		if err != nil {
			return err
		}
		if state == workers.Discarded {
			if err := t.checkDispatchExhaustion(ctx, r.tx, candidate); err != nil {
				return err
			}
			if err := exhausted(); err != nil {
				return err
			}
		}
		if err := t.finishExpired(ctx, r.tx, candidate, state, delay, priorErrors); err != nil {
			return err
		}
		outcome = state
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrStaleEffectLease) {
			return "", ErrExecutionLease
		}
		return "", err
	}
	return outcome, nil
}
