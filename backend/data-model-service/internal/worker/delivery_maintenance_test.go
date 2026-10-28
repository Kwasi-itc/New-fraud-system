package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type maintenanceFunc func(context.Context, int, bool, *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error)

func (f maintenanceFunc) MaintainPage(ctx context.Context, limit int, cleanup bool, after *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
	return f(ctx, limit, cleanup, after)
}

func TestDeliveryMaintenance(t *testing.T) {
	makeScheduler := func(t *testing.T, pages int) *DeliveryMaintenance {
		t.Helper()
		m, err := NewDeliveryMaintenance(1, pages, time.Second, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	t.Run("locked pages advance independent cursors across paced cycles", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			start := time.Now()
			calls := 0
			var prior [2]*ports.DeliveryMaintenanceCursor
			err := makeScheduler(t, 1).Run(ctx, maintenanceFunc(func(ctx context.Context, limit int, cleanup bool, after *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
				stream := calls % 2
				if limit != 1 || cleanup != (stream == 1) || after != prior[stream] {
					t.Fatalf("incorrect stream/cursor at call %d", calls)
				}
				if time.Since(start) != time.Duration(calls/2)*time.Second {
					t.Fatal("cycle was not paced")
				}
				calls++
				prior[stream] = &ports.DeliveryMaintenanceCursor{Queue: "test", Cleanup: cleanup, At: time.Now(), ID: uuid.New()}
				if calls == 6 {
					cancel()
				}
				return 0, prior[stream], nil // Every candidate locked, still advance.
			}))
			if !errors.Is(err, context.Canceled) || calls != 6 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	})
	t.Run("exhausted stream waits until next cycle while other advances", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var order []bool
			start := time.Now()
			err := makeScheduler(t, 3).Run(ctx, maintenanceFunc(func(ctx context.Context, _ int, cleanup bool, after *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
				order = append(order, cleanup)
				if cleanup {
					if after != nil {
						t.Fatal("exhausted cursor not reset")
					}
					return 0, nil, nil
				}
				if len(order) == 5 {
					if time.Since(start) != time.Second || after == nil {
						t.Fatal("cycle/cursor not preserved")
					}
					cancel()
				}
				return 1, &ports.DeliveryMaintenanceCursor{ID: uuid.New()}, nil
			}))
			if !errors.Is(err, context.Canceled) || len(order) != 5 || order[0] || !order[1] || order[2] || order[3] || order[4] {
				t.Fatalf("order=%v err=%v", order, err)
			}
		})
	})
	t.Run("partial failure is fatal and retains evidence", func(t *testing.T) {
		failure := errors.New("commit outcome unknown")
		calls := 0
		err := makeScheduler(t, 1).Run(context.Background(), maintenanceFunc(func(context.Context, int, bool, *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
			calls++
			return 3, nil, failure
		}))
		if calls != 1 || !errors.Is(err, failure) || !strings.Contains(err.Error(), "acknowledged_changes=3") || !strings.Contains(err.Error(), "cleanup=false") {
			t.Fatal(err)
		}
	})
	t.Run("timeout cannot be swallowed and callback is joined", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			exited := false
			err := makeScheduler(t, 1).Run(context.Background(), maintenanceFunc(func(ctx context.Context, _ int, _ bool, _ *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
				<-ctx.Done()
				time.Sleep(time.Second)
				exited = true
				return 0, nil, nil
			}))
			if !exited || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	})
	t.Run("cancellation during wait makes no extra call", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			calls := 0
			done := make(chan error, 1)
			go func() {
				done <- makeScheduler(t, 1).Run(ctx, maintenanceFunc(func(context.Context, int, bool, *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
					calls++
					return 0, nil, nil
				}))
			}()
			synctest.Wait()
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) || calls != 2 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	})
	t.Run("invalid configuration and cancelled start", func(t *testing.T) {
		for _, c := range [][2]int{{0, 1}, {1001, 1}, {1, 0}, {1, 1001}} {
			if _, err := NewDeliveryMaintenance(c[0], c[1], time.Second, time.Second); err == nil {
				t.Fatal(c)
			}
		}
		if _, err := NewDeliveryMaintenance(1, 1, 0, time.Second); err == nil {
			t.Fatal("zero interval")
		}
		if _, err := NewDeliveryMaintenance(1, 1, time.Second, 0); err == nil {
			t.Fatal("zero timeout")
		}
		if err := makeScheduler(t, 1).Run(context.Background(), nil); err == nil {
			t.Fatal("nil store")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := makeScheduler(t, 1).Run(ctx, maintenanceFunc(func(context.Context, int, bool, *ports.DeliveryMaintenanceCursor) (int, *ports.DeliveryMaintenanceCursor, error) {
			t.Fatal("called after cancellation")
			return 0, nil, nil
		}))
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
