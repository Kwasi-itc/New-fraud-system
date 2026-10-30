package work

import (
	"database/sql"
	"sync"
	"time"
)

// DurationHistogram has eight disjoint buckets: <=10ms, <=100ms, <=1s,
// <=10s, <=1m, <=1h, <=24h, and >24h. No samples or identities are retained.
type DurationHistogram struct{ Buckets [8]uint64 }

func (h *DurationHistogram) add(d time.Duration) {
	bounds := [7]time.Duration{10 * time.Millisecond, 100 * time.Millisecond, time.Second, 10 * time.Second, time.Minute, time.Hour, 24 * time.Hour}
	for i, b := range bounds {
		if d <= b {
			h.Buckets[i]++
			return
		}
	}
	h.Buckets[7]++
}

type RunnerCounters struct {
	ClaimCalls, ClaimFailures, Claims                                                       uint64
	HeartbeatCalls, HeartbeatFailures                                                       uint64
	RetryCalls, RetriesConfirmed, ExhaustionsConfirmed, RetryFailures, RetryLeaseRejections uint64
	ReapCalls, ReapFailures, ExpiredConfirmed                                               uint64
	HandlerCalls, HandlerFailures, ProcessesFinished, ProcessFailures, ActiveHandlers       uint64
	ClaimDuration, QueueWait, HandlerDuration                                               DurationHistogram
}

// RunnerMetrics is optional, process-local and safe for concurrent snapshots.
// Use one instance per runner; it has fixed storage and no caller-controlled labels.
// Confirmed counters require successful transaction acknowledgement, so uncertain
// commits can undercount durable outcomes. These are not business accounting.
type RunnerMetrics struct {
	mu       sync.Mutex
	counters RunnerCounters
}

func (m *RunnerMetrics) update(fn func(*RunnerCounters)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.counters)
}
func (m *RunnerMetrics) Snapshot() RunnerCounters {
	if m == nil {
		return RunnerCounters{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters
}

type RunnerTelemetry struct {
	Runner RunnerCounters
	Pool   sql.DBStats
}

// Telemetry reads process memory only. Pool stats cover all users of this DB pool,
// not just this runner. Export must be protected by the service's admin policy.
func (q *Queue) Telemetry(m *RunnerMetrics) RunnerTelemetry {
	return RunnerTelemetry{Runner: m.Snapshot(), Pool: q.db.Stats()}
}
