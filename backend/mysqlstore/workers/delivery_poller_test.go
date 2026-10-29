package workers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func poller(t *testing.T, count int) *DeliveryPoller {
	t.Helper()
	p, err := NewDeliveryPoller(count, time.Second, time.Second, supervisor(t))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDeliveryPoller(t *testing.T) {
	report := func(context.Context, error) error { return nil }
	t.Run("fatal claim cancels and joins sibling execution", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			failure := errors.New("ambiguous claim commit")
			started := make(chan struct{})
			var calls atomic.Int32
			exited := false
			err := poller(t, 2).Run(context.Background(), func(ctx context.Context) (*DeliveryTask, error) {
				if calls.Add(1) == 1 {
					return &DeliveryTask{Heartbeat: func(context.Context) error { return nil }, Execute: func(ctx context.Context) error {
						close(started)
						<-ctx.Done()
						time.Sleep(time.Second)
						exited = true
						return ctx.Err()
					}}, nil
				}
				<-started
				return nil, failure
			}, report)
			if !errors.Is(err, failure) || !exited {
				t.Fatalf("exited=%v err=%v", exited, err)
			}
		})
	})
	t.Run("fixed concurrency no prefetch and joined shutdown", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			var claims, active, exits atomic.Int32
			done := make(chan error, 1)
			go func() {
				done <- poller(t, 3).Run(ctx, func(context.Context) (*DeliveryTask, error) {
					claims.Add(1)
					return &DeliveryTask{Heartbeat: func(context.Context) error { return nil }, Execute: func(ctx context.Context) error {
						active.Add(1)
						<-ctx.Done()
						time.Sleep(2 * time.Second)
						exits.Add(1)
						return ctx.Err()
					}}, nil
				}, report)
			}()
			synctest.Wait()
			if claims.Load() != 3 || active.Load() != 3 {
				t.Fatalf("claims=%d active=%d", claims.Load(), active.Load())
			}
			cancel()
			err := <-done
			if !errors.Is(err, context.Canceled) || exits.Load() != 3 {
				t.Fatalf("exits=%d err=%v", exits.Load(), err)
			}
		})
	})
	t.Run("empty queue waits and shutdown interrupts wait", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			var claims atomic.Int32
			done := make(chan error, 1)
			go func() {
				done <- poller(t, 1).Run(ctx, func(context.Context) (*DeliveryTask, error) { claims.Add(1); return nil, nil }, report)
			}()
			synctest.Wait()
			if claims.Load() != 1 {
				t.Fatal(claims.Load())
			}
			time.Sleep(1500 * time.Millisecond)
			synctest.Wait()
			if claims.Load() != 2 {
				t.Fatal(claims.Load())
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	})
	t.Run("execution failure reported before next claim", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			failure, stop := errors.New("execution failed"), errors.New("stop claiming")
			claims, reports := 0, 0
			start := time.Now()
			err := poller(t, 1).Run(context.Background(), func(context.Context) (*DeliveryTask, error) {
				claims++
				if claims > 1 {
					if reports != 1 || time.Since(start) < time.Second {
						t.Error("retry was not delayed/reported")
					}
					return nil, stop
				}
				return &DeliveryTask{Heartbeat: func(context.Context) error { return nil }, Execute: func(context.Context) error { return failure }}, nil
			}, func(_ context.Context, err error) error {
				reports++
				if !errors.Is(err, failure) {
					t.Error(err)
				}
				return nil
			})
			if !errors.Is(err, stop) || reports != 1 {
				t.Fatalf("reports=%d err=%v", reports, err)
			}
		})
	})
	t.Run("report failure retains both errors", func(t *testing.T) {
		execution, reporting := errors.New("execution"), errors.New("reporting")
		err := poller(t, 1).Run(context.Background(), func(context.Context) (*DeliveryTask, error) {
			return &DeliveryTask{
				Heartbeat: func(context.Context) error { return nil }, Execute: func(context.Context) error { return execution },
			}, nil
		}, func(context.Context, error) error { return reporting })
		if !errors.Is(err, execution) || !errors.Is(err, reporting) {
			t.Fatal(err)
		}
	})
	t.Run("claim timeout cannot launch work", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			err := poller(t, 1).Run(context.Background(), func(ctx context.Context) (*DeliveryTask, error) {
				<-ctx.Done()
				return &DeliveryTask{Heartbeat: func(context.Context) error { t.Error("heartbeat started"); return nil }, Execute: func(context.Context) error { t.Error("work started"); return nil }}, nil
			}, report)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	})
	t.Run("invalid task and configuration", func(t *testing.T) {
		if _, err := NewDeliveryPoller(0, time.Second, time.Second, supervisor(t)); err == nil {
			t.Fatal("zero workers")
		}
		if _, err := NewDeliveryPoller(1025, time.Second, time.Second, supervisor(t)); err == nil {
			t.Fatal("unbounded workers")
		}
		err := poller(t, 1).Run(context.Background(), func(context.Context) (*DeliveryTask, error) { return &DeliveryTask{}, nil }, report)
		if err == nil {
			t.Fatal("invalid task accepted")
		}
		if err := (&DeliveryPoller{}).Run(context.Background(), nil, nil); err == nil {
			t.Fatal("invalid poller accepted")
		}
	})
}
