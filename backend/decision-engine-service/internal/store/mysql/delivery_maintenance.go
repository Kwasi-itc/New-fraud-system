package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

// Cursors are process-local, scoped to one exact pool/database/tenant/kind/queue.
// They carry no payload, business history, authority or durable progress claim.
// Restarting the scheduler safely starts both indexed streams at nil.
type DeliveryMaintenanceCursor struct {
	scope    deliveryMaintenanceScope
	cleanup  bool
	expired  *ExpiredDeliveryCursor
	state    workers.DeliveryState
	terminal *DeliveryCleanupCursor
}

type deliveryMaintenanceScope struct {
	db     *sql.DB
	schema string
	tenant uuid.UUID
	kind   ports.JobKind
	queue  string
}

// DeliveryMaintenanceStore composes existing owner-fenced primitives with the
// shared bounded/paced scheduler; it neither starts a goroutine nor enables a
// worker. Authorization is rechecked by each real SQL primitive, not a cursor.
type DeliveryMaintenanceStore struct {
	scope   deliveryMaintenanceScope
	expired func(context.Context, *ExpiredDeliveryCursor, int) ([]ExpiredDelivery, error)
	rescue  func(context.Context, ExpiredDelivery) (workers.DeliveryState, error)
	cleanup func(context.Context, workers.DeliveryState, *DeliveryCleanupCursor, int) (int, *DeliveryCleanupCursor, error)
}

var _ workers.MaintenanceStore[DeliveryMaintenanceCursor] = (*DeliveryMaintenanceStore)(nil)

func maintenanceScope(s *EffectDeliveryStore) deliveryMaintenanceScope {
	return deliveryMaintenanceScope{s.db, s.schema, s.tenant, s.kind, s.queue}
}

func NewEffectDeliveryMaintenance(s *EffectDeliveryStore, exhausted EffectExhaustionHandler) (*DeliveryMaintenanceStore, error) {
	if s == nil || s.db == nil || s.authorize == nil || s.executionKinds || effectOwnerTable(s.kind) == "" || exhausted == nil {
		return nil, errors.New("scoped effect transport and real owner exhaustion handler required")
	}
	return &DeliveryMaintenanceStore{scope: maintenanceScope(s), expired: s.Expired, rescue: func(ctx context.Context, v ExpiredDelivery) (workers.DeliveryState, error) {
		return s.Rescue(ctx, v, exhausted)
	}, cleanup: s.CleanupTerminalPage}, nil
}

func NewExecutionDeliveryMaintenance(s *ExecutionDeliveryStore, owner *ExecutionStore) (*DeliveryMaintenanceStore, error) {
	if s == nil || s.transport == nil || owner == nil || owner.configuration == nil || owner.authorize == nil {
		return nil, errors.New("initialized execution transport and owner required")
	}
	t, c := s.transport, owner.configuration
	if !t.executionKinds || t.authorize == nil || t.db != c.db || t.schema != c.schema || t.tenant != c.tenant || owner.policy.Queues[t.kind].Name != t.queue {
		return nil, errors.New("maintenance owner must bind the same pool/schema/tenant/kind/queue")
	}
	return &DeliveryMaintenanceStore{scope: maintenanceScope(t), expired: s.Expired, rescue: func(ctx context.Context, v ExpiredDelivery) (workers.DeliveryState, error) {
		return s.Rescue(ctx, owner, v)
	}, cleanup: s.CleanupTerminalPage}, nil
}

// MaintainPage processes at most 100 candidates, with separate discovery and
// short per-owner transactions. Counts report only acknowledged commits, even
// when a subsequent candidate fails. All real failures terminate the scheduler;
// no ambiguous commit, permission, missing owner or callback failure is ignored.
func (s *DeliveryMaintenanceStore) MaintainPage(ctx context.Context, limit int, cleanup bool, after *DeliveryMaintenanceCursor) (int, *DeliveryMaintenanceCursor, error) {
	if s == nil || s.expired == nil || s.rescue == nil || s.cleanup == nil || limit < 1 || limit > 100 {
		return 0, nil, errors.New("initialized decision maintenance and page size 1..100 required")
	}
	if after != nil && (after.scope != s.scope || after.cleanup != cleanup || (cleanup && after.expired != nil) || (!cleanup && (after.terminal != nil || after.state != ""))) {
		return 0, nil, errors.New("maintenance cursor belongs to another scope/stream")
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	if cleanup {
		state := workers.Completed
		var cursor *DeliveryCleanupCursor
		if after != nil {
			state, cursor = after.state, after.terminal
		}
		if _, err := deliveryRetention(state); err != nil {
			return 0, nil, err
		}
		n, next, err := s.cleanup(ctx, state, cursor, limit)
		if err != nil {
			return n, nil, err
		}
		if next != nil {
			return n, &DeliveryMaintenanceCursor{scope: s.scope, cleanup: true, state: state, terminal: next}, nil
		}
		switch state {
		case workers.Completed:
			state = workers.Cancelled
		case workers.Cancelled:
			state = workers.Discarded
		case workers.Discarded:
			return n, nil, nil
		}
		return n, &DeliveryMaintenanceCursor{scope: s.scope, cleanup: true, state: state}, nil
	}
	var cursor *ExpiredDeliveryCursor
	if after != nil {
		cursor = after.expired
	}
	candidates, err := s.expired(ctx, cursor, limit)
	if err != nil {
		return 0, nil, err
	}
	if len(candidates) > limit {
		return 0, nil, errors.New("expired discovery exceeded page contract")
	}
	acknowledged := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return acknowledged, nil, err
		}
		state, err := s.rescue(ctx, candidate)
		// Only bare observation-fence conflicts are harmless races. A wrapped
		// rollback/commit/authorization failure must not be swallowed merely
		// because errors.Is also happens to find a stale-lease sentinel.
		if err == ErrStaleEffectLease || err == ErrExecutionLease {
			continue
		}
		if err != nil {
			return acknowledged, nil, err
		}
		if state != workers.Available && state != workers.Discarded {
			return acknowledged, nil, errors.New("rescue did not produce a valid recovery outcome")
		}
		acknowledged++
	}
	if len(candidates) < limit {
		return acknowledged, nil, nil
	}
	last := candidates[len(candidates)-1]
	return acknowledged, &DeliveryMaintenanceCursor{scope: s.scope, expired: &ExpiredDeliveryCursor{LeaseUntil: last.LeaseUntil, ID: last.ID}}, nil
}
