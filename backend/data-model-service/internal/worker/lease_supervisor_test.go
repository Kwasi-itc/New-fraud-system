package worker

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func supervisor(t *testing.T) *LeaseSupervisor {
	t.Helper()
	s, err := NewLeaseSupervisor(30*time.Second, 5*time.Second, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLeaseSupervisor(t *testing.T) {
	t.Run("initial renewal timeout prevents execution", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			err := supervisor(t).Run(context.Background(), func(ctx context.Context) error { <-ctx.Done(); return nil }, func(context.Context) error { t.Error("work started"); return nil })
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	})
	t.Run("successful work cannot hide simultaneous lease loss", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			lost := errors.New("lease lost during finalization")
			calls := 0
			err := supervisor(t).Run(context.Background(), func(context.Context) error {
				calls++
				if calls > 1 {
					return lost
				}
				return nil
			}, func(ctx context.Context) error { <-ctx.Done(); return nil })
			if !errors.Is(err, lost) {
				t.Fatal(err)
			}
		})
	})
	t.Run("panic joins heartbeat and propagates", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			started := make(chan struct{})
			calls, exited := 0, false
			defer func() {
				if recover() != "executor panic" || !exited {
					t.Error("panic swallowed or heartbeat leaked")
				}
			}()
			_ = supervisor(t).Run(context.Background(), func(ctx context.Context) error {
				calls++
				if calls > 1 {
					close(started)
					<-ctx.Done()
					exited = true
					return ctx.Err()
				}
				return nil
			}, func(context.Context) error { <-started; panic("executor panic") })
		})
	})
	t.Run("verify before work and retain execution error", func(t *testing.T) {
		failure := errors.New("claim expired")
		err := supervisor(t).Run(context.Background(), func(context.Context) error { return failure }, func(context.Context) error { t.Fatal("started with invalid lease"); return nil })
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		err = supervisor(t).Run(context.Background(), func(context.Context) error { return nil }, func(context.Context) error { return failure })
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	})
	t.Run("renew while working and stop on completion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			calls := 0
			err := supervisor(t).Run(context.Background(), func(context.Context) error { calls++; return nil }, func(context.Context) error { time.Sleep(12 * time.Second); return nil })
			if err != nil || calls != 3 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			time.Sleep(time.Minute)
			if calls != 3 {
				t.Fatalf("heartbeat leaked: %d", calls)
			}
		})
	})
	t.Run("lost lease cancels work and waits for exit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			lost := errors.New("lost lease")
			calls := 0
			exited := false
			err := supervisor(t).Run(context.Background(), func(context.Context) error {
				calls++
				if calls == 2 {
					return lost
				}
				return nil
			}, func(ctx context.Context) error {
				<-ctx.Done()
				if !errors.Is(context.Cause(ctx), lost) {
					t.Error(context.Cause(ctx))
				}
				time.Sleep(10 * time.Second)
				exited = true
				return ctx.Err()
			})
			if !exited || !errors.Is(err, lost) || !errors.Is(err, context.Canceled) {
				t.Fatalf("exited=%v err=%v", exited, err)
			}
		})
	})
	t.Run("heartbeat timeout fails closed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			calls := 0
			err := supervisor(t).Run(context.Background(), func(ctx context.Context) error {
				calls++
				if calls > 1 {
					<-ctx.Done()
				}
				return nil
			}, func(ctx context.Context) error { <-ctx.Done(); return nil })
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	})
	t.Run("work timeout cannot return success", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			err := supervisor(t).Run(context.Background(), func(context.Context) error { return nil }, func(ctx context.Context) error { <-ctx.Done(); return nil })
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	})
	t.Run("shutdown joins active heartbeat", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			started := make(chan struct{})
			calls := 0
			exited := false
			err := supervisor(t).Run(ctx, func(ctx context.Context) error {
				calls++
				if calls > 1 {
					close(started)
					<-ctx.Done()
					time.Sleep(time.Second)
					exited = true
					return ctx.Err()
				}
				return nil
			}, func(ctx context.Context) error { <-started; cancel(); <-ctx.Done(); return nil })
			if !exited || !errors.Is(err, context.Canceled) {
				t.Fatalf("exited=%v err=%v", exited, err)
			}
		})
	})
	t.Run("completion cancels inflight heartbeat normally", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			started := make(chan struct{})
			calls := 0
			err := supervisor(t).Run(context.Background(), func(ctx context.Context) error {
				calls++
				if calls > 1 {
					close(started)
					<-ctx.Done()
					return ctx.Err()
				}
				return nil
			}, func(context.Context) error { <-started; return nil })
			if err != nil {
				t.Fatal(err)
			}
		})
	})
	t.Run("already cancelled does not touch storage", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := supervisor(t).Run(ctx, func(context.Context) error { t.Fatal("heartbeat called"); return nil }, func(context.Context) error { t.Fatal("work called"); return nil })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("invalid configuration", func(t *testing.T) {
		for _, v := range [][4]time.Duration{{0, 1, 1, 1}, {30, 11, 1, 1}, {30, 5, 6, 1}, {30, 5, 1, 0}} {
			if _, err := NewLeaseSupervisor(v[0], v[1], v[2], v[3]); err == nil {
				t.Fatal(v)
			}
		}
		if err := (&LeaseSupervisor{}).Run(context.Background(), nil, nil); err == nil {
			t.Fatal("accepted zero configuration")
		}
	})
}
