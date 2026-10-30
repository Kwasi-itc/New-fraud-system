package work

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunnerMetricsConcurrentSnapshots(t *testing.T) {
	var metrics RunnerMetrics
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				metrics.update(func(m *RunnerCounters) { m.ClaimCalls++; m.Claims++; m.QueueWait.add(time.Second) })
				s := metrics.Snapshot()
				if s.ClaimCalls != s.Claims {
					t.Error("torn snapshot")
				}
			}
		}()
	}
	wg.Wait()
	snapshot := metrics.Snapshot()
	if snapshot.Claims != 8000 || snapshot.QueueWait.Buckets[2] != 8000 {
		t.Fatal(snapshot)
	}
	snapshot.QueueWait.Buckets[2] = 0
	if metrics.Snapshot().QueueWait.Buckets[2] != 8000 {
		t.Fatal("mutable snapshot")
	}
	raw, err := json.Marshal(metrics.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"payload", "tenant", "token", "error", "resource", "queue_name"} {
		if strings.Contains(strings.ToLower(string(raw)), secret) {
			t.Fatal("sensitive metric field", secret)
		}
	}
	var absent *RunnerMetrics
	absent.update(func(*RunnerCounters) { t.Fatal("disabled metrics callback") })
	if absent.Snapshot() != (RunnerCounters{}) {
		t.Fatal("disabled metrics")
	}
}
func TestDurationHistogramBoundaries(t *testing.T) {
	var h DurationHistogram
	bounds := []time.Duration{10 * time.Millisecond, 100 * time.Millisecond, time.Second, 10 * time.Second, time.Minute, time.Hour, 24 * time.Hour}
	for _, d := range bounds {
		h.add(d)
		h.add(d + time.Nanosecond)
	}
	if h.Buckets[0] != 1 || h.Buckets[7] != 1 {
		t.Fatal(h)
	}
	for _, count := range h.Buckets[1:7] {
		if count != 2 {
			t.Fatal(h)
		}
	}
}
