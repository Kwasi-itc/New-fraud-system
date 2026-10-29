package workers

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

type pageFunc func(context.Context, int, bool, *int) (int, *int, error)

func (f pageFunc) MaintainPage(ctx context.Context, n int, cleanup bool, cursor *int) (int, *int, error) {
	return f(ctx, n, cleanup, cursor)
}

func TestMaintenanceScheduler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m, err := NewMaintenanceScheduler[int](1, 1, time.Second, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		var prior [2]*int
		calls := 0
		start := time.Now()
		err = m.Run(ctx, pageFunc(func(ctx context.Context, n int, cleanup bool, after *int) (int, *int, error) {
			i := calls % 2
			if cleanup != (i == 1) || after != prior[i] || n != 1 {
				t.Fatal("streams/cursors mixed")
			}
			if time.Since(start) != time.Duration(calls/2)*time.Second {
				t.Fatal("maintenance failure hot loop")
			}
			next := calls
			prior[i] = &next
			calls++
			if calls == 6 {
				cancel()
			}
			return 0, prior[i], nil
		}))
		if !errors.Is(err, context.Canceled) || calls != 6 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	})
	for _, values := range [][2]int{{0, 1}, {1, 0}, {1001, 1}, {1, 1001}} {
		if _, err := NewMaintenanceScheduler[int](values[0], values[1], time.Second, time.Second); err == nil {
			t.Fatal("unbounded maintenance accepted")
		}
	}
	failure := errors.New("ambiguous maintenance commit")
	m, _ := NewMaintenanceScheduler[int](1, 1, time.Second, time.Second)
	if err := m.Run(context.Background(), pageFunc(func(context.Context, int, bool, *int) (int, *int, error) { return 2, nil, failure })); !errors.Is(err, failure) {
		t.Fatal("maintenance error lost")
	}
}
