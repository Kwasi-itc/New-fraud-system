package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestProcessReceiptCollision(t *testing.T) {
	shared, f := fullRecordFixture(t)
	o, _ := fullOwner(t, shared, f, "runtime")
	o.receipts.digest = func([]byte) [32]byte { return [32]byte{42} }
	o.receipts.limits.CollisionRows = 2
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	for i, key := range []string{"key", "key "} {
		c := fullCommand(f)
		c.Records = []map[string]any{{"object_id": fmt.Sprintf("record-%d", i)}}
		if _, e := o.Execute(ctx, c, key); e != nil {
			t.Fatal(e)
		}
		r, e := o.Execute(ctx, c, key)
		if e != nil || !r.Replayed {
			t.Fatal(r, e)
		}
	}
	c := fullCommand(f)
	c.Records = []map[string]any{{"object_id": "overflow"}}
	if _, e := o.Execute(ctx, c, "third"); !errors.Is(e, ErrReceiptLimit) {
		t.Fatal(e)
	}
	if got := fullDurableCounts(t, f); got.Records != 2 || got.PublicReceipts != 2 || got.Audits != 2 || got.Outbox != 2 {
		t.Fatal(got)
	}
}

// These small closed-loop runs characterize the local fixture, not capacity or SLOs.
func TestProcessMeasuredWorkloads(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("worker_%t", mixed), func(t *testing.T) {
			_, f, cfg := processFixture(t)
			api := startProcess(t, cfg, false)
			w := processWork(t, f, cfg)
			if mixed {
				startProcess(t, cfg, true)
			}
			for _, hot := range []bool{true, false} {
				for _, concurrency := range []int{1, 4} {
					const count = 32
					label := fmt.Sprintf("hot_%t_c%d", hot, concurrency)
					var workIDs []WorkInput
					if mixed {
						for i := 0; i < 8; i++ {
							in := workInput(DeferredWork)
							in.Payload = []byte(fmt.Sprintf(`{"object_id":"work-%s-%d"}`, label, i))
							if _, e := w.Create(WithServiceCredential(context.Background(), processToken), in); e != nil {
								t.Fatal(e)
							}
							workIDs = append(workIDs, in)
						}
					}
					before := fullDurableCounts(t, f)
					latencies := make([]float64, count)
					codes := make([]int, count)
					errs := make([]error, count)
					var mu sync.Mutex
					peakConnections, lockWaitSamples := 0, 0
					samples := 0
					sampleError := make(chan error, 1)
					stop := make(chan struct{})
					sampleDone := make(chan struct{})
					go func() {
						defer close(sampleDone)
						ticker := time.NewTicker(10 * time.Millisecond)
						defer ticker.Stop()
						for {
							select {
							case <-stop:
								return
							case <-ticker.C:
								var connections, waits int
								if e := f.db.QueryRow("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER=?", cfg["User"]).Scan(&connections); e != nil {
									sampleError <- e
									return
								}
								if e := f.db.QueryRow("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE WHERE l.OBJECT_SCHEMA=?", f.database).Scan(&waits); e != nil {
									sampleError <- e
									return
								}
								mu.Lock()
								if connections > peakConnections {
									peakConnections = connections
								}
								lockWaitSamples += waits
								samples++
								mu.Unlock()
							}
						}
					}()
					jobs := make(chan int)
					var wg sync.WaitGroup
					start := time.Now()
					for n := 0; n < concurrency; n++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							for i := range jobs {
								id := label
								if !hot {
									id = fmt.Sprintf("%s-%d", label, i)
								}
								body := []byte(fmt.Sprintf(`{"object_id":%q}`, id))
								began := time.Now()
								codes[i], _, errs[i] = processRequest(context.Background(), api, "POST", recordPath(f), id, body)
								latencies[i] = float64(time.Since(began).Microseconds()) / 1000
							}
						}()
					}
					for i := 0; i < count; i++ {
						jobs <- i
					}
					close(jobs)
					wg.Wait()
					elapsed := time.Since(start)
					close(stop)
					<-sampleDone
					select {
					case e := <-sampleError:
						t.Fatal("measurement collection failed", e)
					default:
					}
					if samples == 0 {
						t.Fatal("measurement collected no resource samples")
					}
					for i := range codes {
						if codes[i] != 200 || errs[i] != nil {
							t.Fatal(i, codes[i], errs[i])
						}
					}
					for _, in := range workIDs {
						waitWorkState(t, f.db, in.ID, "completed")
					}
					after := fullDurableCounts(t, f)
					want := int64(count)
					if hot {
						want = 1
					}
					if after.PublicReceipts-before.PublicReceipts != want {
						t.Fatal(before, after)
					}
					if peakConnections > cfg["Pool"].(int)*map[bool]int{true: 2, false: 1}[mixed]+1 {
						t.Fatal("connection budget exceeded", peakConnections)
					}
					sorted := append([]float64(nil), latencies...)
					sort.Float64s(sorted)
					report := map[string]any{"configuration": label, "worker": mixed, "requests": count, "concurrency": concurrency, "seconds": elapsed.Seconds(), "requests_per_second": count / elapsed.Seconds(), "p50_ms": sorted[15], "p95_ms": sorted[30], "max_ms": sorted[31], "latency_ms": latencies, "peak_account_connections": peakConnections, "account_includes_fixture_pool": true, "sampled_lock_waits": lockWaitSamples, "before": before, "after": after, "seed": "deterministic-v1", "api_pool": cfg["Pool"], "worker_pool": cfg["Pool"], "limitation": "32 closed-loop samples; race instrumentation; no capacity or production inference"}
					data, e := json.Marshal(report)
					if e != nil {
						t.Fatal(e)
					}
					t.Log("MEASUREMENT " + string(data))
				}
			}
		})
	}
}
