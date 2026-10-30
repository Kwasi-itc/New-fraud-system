package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func maintenanceFixture() *DeliveryMaintenanceStore {
	return &DeliveryMaintenanceStore{
		scope:   deliveryMaintenanceScope{tenant: uuid.New()},
		expired: func(context.Context, *ExpiredDeliveryCursor, int) ([]ExpiredDelivery, error) { return nil, nil },
		rescue:  func(context.Context, ExpiredDelivery) (workers.DeliveryState, error) { return workers.Available, nil },
		cleanup: func(context.Context, workers.DeliveryState, *DeliveryCleanupCursor, int) (int, *DeliveryCleanupCursor, error) {
			return 0, nil, nil
		},
	}
}

func TestDeliveryMaintenanceRecoveryProgressAndPartialFailure(t *testing.T) {
	s := maintenanceFixture()
	candidates := []ExpiredDelivery{{ID: uuid.New(), LeaseUntil: time.Now().UTC().Truncate(time.Microsecond)}, {ID: uuid.New(), LeaseUntil: time.Now().UTC().Truncate(time.Microsecond)}}
	s.expired = func(_ context.Context, after *ExpiredDeliveryCursor, limit int) ([]ExpiredDelivery, error) {
		if limit != 2 {
			t.Fatal(limit)
		}
		if after == nil {
			return candidates, nil
		}
		if after.ID != candidates[1].ID {
			t.Fatal("cursor did not advance over stale candidate", after)
		}
		return nil, nil
	}
	s.rescue = func(_ context.Context, v ExpiredDelivery) (workers.DeliveryState, error) {
		if v.ID == candidates[1].ID {
			return "", ErrExecutionLease
		}
		return workers.Available, nil
	}
	n, cursor, err := s.MaintainPage(context.Background(), 2, false, nil)
	if err != nil || n != 1 || cursor == nil {
		t.Fatal(n, cursor, err)
	}
	if n, next, err := s.MaintainPage(context.Background(), 2, false, cursor); err != nil || n != 0 || next != nil {
		t.Fatal(n, next, err)
	}
	failed := &mysqlstore.CommitError{Cause: errors.New("uncertain commit")}
	s.rescue = func(_ context.Context, v ExpiredDelivery) (workers.DeliveryState, error) {
		if v.ID == candidates[1].ID {
			return "", failed
		}
		return workers.Discarded, nil
	}
	if n, next, err := s.MaintainPage(context.Background(), 2, false, nil); n != 1 || next != nil || !errors.Is(err, failed) {
		t.Fatal("lost acknowledged count or swallowed uncertain commit", n, next, err)
	}
	s.rescue = func(context.Context, ExpiredDelivery) (workers.DeliveryState, error) {
		return "", errors.Join(ErrExecutionLease, errors.New("rollback failed"))
	}
	if _, _, err := s.MaintainPage(context.Background(), 2, false, nil); err == nil {
		t.Fatal("composite rollback failure swallowed as stale candidate")
	}
}

func TestDeliveryMaintenanceCleanupCyclesAndScope(t *testing.T) {
	s := maintenanceFixture()
	states := []workers.DeliveryState{}
	s.cleanup = func(_ context.Context, state workers.DeliveryState, _ *DeliveryCleanupCursor, _ int) (int, *DeliveryCleanupCursor, error) {
		states = append(states, state)
		return 0, nil, nil // Zero deletions is not exhaustion of the whole stream.
	}
	var cursor *DeliveryMaintenanceCursor
	for i := 0; i < 3; i++ {
		var err error
		_, cursor, err = s.MaintainPage(context.Background(), 1, true, cursor)
		if err != nil || (i < 2 && cursor == nil) {
			t.Fatal(cursor, err)
		}
	}
	if cursor != nil || len(states) != 3 || states[0] != workers.Completed || states[1] != workers.Cancelled || states[2] != workers.Discarded {
		t.Fatal("terminal state starvation/order", states, cursor)
	}
	locked := &DeliveryCleanupCursor{ID: uuid.New(), FinishedAt: time.Now().UTC().Truncate(time.Microsecond)}
	s.cleanup = func(context.Context, workers.DeliveryState, *DeliveryCleanupCursor, int) (int, *DeliveryCleanupCursor, error) {
		return 0, locked, nil
	}
	_, cursor, err := s.MaintainPage(context.Background(), 1, true, nil)
	if err != nil || cursor == nil || cursor.terminal != locked {
		t.Fatal("locked discovery cursor lost", err)
	}
	if _, _, err := s.MaintainPage(context.Background(), 1, false, cursor); err == nil {
		t.Fatal("cleanup cursor used for recovery")
	}
	if _, _, err := maintenanceFixture().MaintainPage(context.Background(), 1, true, cursor); err == nil {
		t.Fatal("cross-tenant cursor accepted")
	}
	for _, size := range []int{0, 101} {
		if _, _, err := s.MaintainPage(context.Background(), size, true, nil); err == nil {
			t.Fatal("unbounded page", size)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.MaintainPage(cancelled, 1, true, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if _, err := NewEffectDeliveryMaintenance(nil, nil); err == nil {
		t.Fatal("missing real effect handler accepted")
	}
	if _, err := NewExecutionDeliveryMaintenance(nil, nil); err == nil {
		t.Fatal("missing execution owner accepted")
	}
}
