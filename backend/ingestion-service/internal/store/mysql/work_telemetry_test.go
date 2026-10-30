package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
)

func runTelemetry(t *testing.T, w *WorkOwner, metrics *work.RunnerMetrics, handle work.Handler) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	done := make(chan error, 1)
	go func() {
		done <- w.Queue().Run(ctx, work.RunnerConfig{Queue: "deferred", Concurrency: 1, Lease: 6 * time.Second, Timeout: 8 * time.Second, Poll: 10 * time.Millisecond, RetryBase: time.Second, RetryMax: time.Second, Metrics: metrics}, handle, func(error) {})
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Error(err)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}
func awaitTelemetry(t *testing.T, m *work.RunnerMetrics, ready func(work.RunnerCounters) bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ready(m.Snapshot()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("telemetry condition timed out", m.Snapshot())
}
func histogramCount(h work.DurationHistogram) uint64 {
	var n uint64
	for _, v := range h.Buckets {
		n += v
	}
	return n
}

func TestWorkTelemetryCompletionAndPool(t *testing.T) {
	f, w := workFixture(t)
	in := workInput(DeferredWork)
	if _, err := w.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	var metrics work.RunnerMetrics
	stop := runTelemetry(t, w, &metrics, w.Handle)
	awaitTelemetry(t, &metrics, func(m work.RunnerCounters) bool { return m.ProcessesFinished == 1 })
	stop()
	s := w.Queue().Telemetry(&metrics)
	if s.Runner.Claims != 1 || s.Runner.HandlerFailures != 0 || s.Runner.ProcessFailures != 0 || histogramCount(s.Runner.QueueWait) != 1 || histogramCount(s.Runner.HandlerDuration) != 1 {
		t.Fatal(s)
	}
	assertOwnerCounts(t, f, 1, 1, 1, 0) // Private execution receipt, no public receipt.
	var receipts int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM " + w.owner.receipts.schema + ".ingestion_execution_receipts").Scan(&receipts); err != nil || receipts != 1 {
		t.Fatal(receipts, err)
	}
	f.db.SetMaxOpenConns(1)
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := f.db.PingContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		conn.Close()
		t.Fatal(err)
	}
	pool := w.Queue().Telemetry(&metrics).Pool
	conn.Close()
	if pool.MaxOpenConnections != 1 || pool.InUse != 1 || pool.WaitCount < 1 || pool.WaitDuration <= 0 {
		t.Fatal(pool)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), in.ID.String()) || strings.Contains(string(raw), "object_id") {
		t.Fatal("identity/payload leakage")
	}
}

func TestWorkTelemetryRetriesAndExhaustion(t *testing.T) {
	f, w := workFixture(t)
	w.maxAttempts = 2
	if _, err := w.Create(context.Background(), workInput(DeferredWork)); err != nil {
		t.Fatal(err)
	}
	var metrics work.RunnerMetrics
	stop := runTelemetry(t, w, &metrics, func(context.Context, work.Lease) error { return errors.New("private-payload-secret") })
	awaitTelemetry(t, &metrics, func(m work.RunnerCounters) bool { return m.ExhaustionsConfirmed == 1 && m.ProcessesFinished == 2 })
	stop()
	s := metrics.Snapshot()
	if s.RetriesConfirmed != 1 || s.RetryCalls != 2 || s.HandlerFailures != 2 || s.ProcessFailures != 2 || s.ActiveHandlers != 0 {
		t.Fatal(s)
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "private-payload-secret") {
		t.Fatal("raw error leaked")
	}
	assertOwnerCounts(t, f, 0, 0, 0, 0)
}

func TestWorkTelemetryExpiredAndClaimFailure(t *testing.T) {
	for _, scenario := range []string{"expired", "claim_failure", "retry_failure", "reap_failure"} {
		t.Run(scenario, func(t *testing.T) {
			f, w := workFixture(t)
			w.maxAttempts = 1
			if _, err := w.Create(context.Background(), workInput(DeferredWork)); err != nil {
				t.Fatal(err)
			}
			s := w.owner.receipts.schema
			if scenario == "expired" || scenario == "reap_failure" {
				l := claimWork(t, w, DeferredWork)
				if _, err := f.db.Exec("UPDATE "+s+".ingestion_work_jobs SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(l.ID)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "claim_failure" || scenario == "reap_failure" {
				if _, err := f.db.Exec("CREATE TRIGGER " + s + ".deny_claim BEFORE UPDATE ON " + s + ".ingestion_work_jobs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private claim failure'"); err != nil {
					t.Fatal(err)
				}
			}
			var metrics work.RunnerMetrics
			stop := runTelemetry(t, w, &metrics, func(context.Context, work.Lease) error {
				if scenario == "retry_failure" {
					_, err := f.db.Exec("CREATE TRIGGER " + s + ".deny_retry BEFORE UPDATE ON " + s + ".ingestion_work_jobs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private retry failure'")
					if err != nil {
						t.Error(err)
					}
				}
				return errors.New("test failure")
			})
			awaitTelemetry(t, &metrics, func(m work.RunnerCounters) bool {
				switch scenario {
				case "expired":
					return m.ExpiredConfirmed == 1
				case "claim_failure":
					return m.ClaimFailures > 0
				case "reap_failure":
					return m.ReapFailures > 0
				default:
					return m.RetryFailures > 0
				}
			})
			stop()
			m := metrics.Snapshot()
			if scenario == "claim_failure" && (m.Claims != 0 || m.HandlerCalls != 0) {
				t.Fatal(m)
			}
			if scenario == "reap_failure" && m.ExpiredConfirmed != 0 {
				t.Fatal(m)
			}
			if scenario == "retry_failure" && (m.RetriesConfirmed != 0 || m.ExhaustionsConfirmed != 0) {
				t.Fatal(m)
			}
			assertOwnerCounts(t, f, 0, 0, 0, 0)
		})
	}
}

func TestWorkTelemetryHeartbeatFence(t *testing.T) {
	f, w := workFixture(t)
	if _, err := w.Create(context.Background(), workInput(DeferredWork)); err != nil {
		t.Fatal(err)
	}
	var metrics work.RunnerMetrics
	stop := runTelemetry(t, w, &metrics, func(ctx context.Context, l work.Lease) error {
		if _, err := f.db.Exec("UPDATE "+w.owner.receipts.schema+".ingestion_work_jobs SET lease_token=UUID_TO_BIN(UUID()) WHERE id=?", mysqlstore.UUID(l.ID)); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	awaitTelemetry(t, &metrics, func(m work.RunnerCounters) bool { return m.RetryLeaseRejections == 1 && m.ProcessesFinished == 1 })
	stop()
	m := metrics.Snapshot()
	if m.HeartbeatFailures != 1 || m.HeartbeatCalls != 1 || m.RetriesConfirmed != 0 || m.ActiveHandlers != 0 {
		t.Fatal(m)
	}
	assertOwnerCounts(t, f, 0, 0, 0, 0)
}

func TestWorkTelemetryQueueWaitUsesDatabaseTime(t *testing.T) {
	f, w := workFixture(t)
	for _, old := range []bool{false, true} {
		in := workInput(DeferredWork)
		if _, err := w.Create(context.Background(), in); err != nil {
			t.Fatal(err)
		}
		at := "UTC_TIMESTAMP(6)-INTERVAL 2 SECOND"
		if old {
			at = "'1000-01-01 00:00:00'"
		}
		if _, err := f.db.Exec("UPDATE "+w.owner.receipts.schema+".ingestion_work_jobs SET available_at="+at+" WHERE id=?", mysqlstore.UUID(in.ID)); err != nil {
			t.Fatal(err)
		}
		l := claimWork(t, w, DeferredWork)
		if old {
			if l.QueueWait != time.Duration(1<<63-1) {
				t.Fatal("overflow", l.QueueWait)
			}
		} else if l.QueueWait < 2*time.Second || l.QueueWait > 30*time.Second {
			t.Fatal(l.QueueWait)
		}
	}
}
