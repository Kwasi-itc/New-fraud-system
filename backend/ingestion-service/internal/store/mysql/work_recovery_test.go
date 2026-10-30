package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
)

func TestIngestionWorkRecovery(t *testing.T) {
	for _, mode := range []string{"retry", "expiry", "noop"} {
		t.Run(mode, func(t *testing.T) {
			f, w := workFixture(t)
			w.maxAttempts = 1
			ctx := context.Background()
			in := workInput(DeferredWork)
			if _, err := w.Create(ctx, in); err != nil {
				t.Fatal(err)
			}
			if mode == "noop" {
				runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				done := make(chan error, 1)
				observed := make(chan error, 16)
				go func() {
					done <- w.Queue().Run(runCtx, work.RunnerConfig{Queue: "deferred", Concurrency: 1, Lease: 3 * time.Second, Timeout: time.Second, Poll: 10 * time.Millisecond, RetryBase: time.Second, RetryMax: time.Second}, func(context.Context, work.Lease) error { return nil }, func(err error) { observed <- err })
				}()
				select {
				case err := <-observed:
					if err == nil {
						t.Fatal("missing no-op failure")
					}
				case <-runCtx.Done():
					t.Fatal("no-op worker was not rejected")
				}
				cancel()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				l := claimWork(t, w, DeferredWork)
				if err := w.Queue().Heartbeat(ctx, l, time.Minute); err != nil {
					t.Fatal(err)
				}
				if mode == "retry" {
					if err := w.Queue().Retry(ctx, l, time.Second, "test_failure"); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := f.db.Exec("UPDATE "+w.owner.receipts.schema+".ingestion_work_jobs SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(l.ID)); err != nil {
						t.Fatal(err)
					}
					if n, err := w.Queue().Reap(ctx, 1, time.Second); err != nil || n != 1 {
						t.Fatal(n, err)
					}
				}
				if err := w.Handle(ctx, l); !errors.Is(err, work.ErrLease) {
					t.Fatal("stale handler", err)
				}
			}
			r, err := w.Get(ctx, in.ID)
			if err != nil || r.State != "discarded" || r.CompletedAt == nil {
				t.Fatal(r, err)
			}
			var active int
			if err := f.db.QueryRow("SELECT COUNT(*) FROM " + w.owner.receipts.schema + ".ingestion_work_reservations").Scan(&active); err != nil || active != 0 {
				t.Fatal(active, err)
			}
			assertOwnerCounts(t, f, 0, 0, 0, 0)
		})
	}
}
