package workers

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// DeliveryMetrics measures the host, NOT durable completed/discarded job state.
// It has no customer labels, callbacks or retries, and must not be copied after
// use. Concurrent multi-counter snapshots are approximate; compare the same
// process/host instance and reconcile totals only after it has quiesced.
type DeliveryMetrics struct {
	claims, empty, claimFailures                          atomic.Int64
	runs, active, succeeded, failed                       atomic.Int64
	heartbeats, heartbeatFailures, heartbeatCancellations atomic.Int64
	claimMicros, runMicros                                atomic.Int64
}

type DeliveryMetricsSnapshot struct {
	ClaimCalls             int64 `json:"claim_calls"`
	EmptyPolls             int64 `json:"empty_polls"`
	ClaimFailures          int64 `json:"claim_failures"`
	HostRuns               int64 `json:"host_runs"`
	InFlight               int64 `json:"in_flight"`
	HostRunsSucceeded      int64 `json:"host_runs_succeeded"`
	HostRunsFailed         int64 `json:"host_runs_failed"`
	Heartbeats             int64 `json:"heartbeats"`
	HeartbeatFailures      int64 `json:"heartbeat_failures"`
	HeartbeatCancellations int64 `json:"heartbeat_cancellations"`
	ClaimMicros            int64 `json:"claim_micros"`
	RunMicros              int64 `json:"run_micros"`
}

func (m *DeliveryMetrics) Snapshot() DeliveryMetricsSnapshot {
	if m == nil {
		return DeliveryMetricsSnapshot{}
	}
	return DeliveryMetricsSnapshot{m.claims.Load(), m.empty.Load(), m.claimFailures.Load(), m.runs.Load(), m.active.Load(), m.succeeded.Load(), m.failed.Load(), m.heartbeats.Load(), m.heartbeatFailures.Load(), m.heartbeatCancellations.Load(), m.claimMicros.Load(), m.runMicros.Load()}
}

func (m *DeliveryMetrics) run(ctx context.Context, s *LeaseSupervisor, task *DeliveryTask) (result error) {
	start := time.Now()
	m.runs.Add(1)
	m.active.Add(1)
	returned := false
	defer func() {
		m.active.Add(-1)
		m.runMicros.Add(time.Since(start).Microseconds())
		if !returned || result != nil {
			m.failed.Add(1)
		} else {
			m.succeeded.Add(1)
		}
	}()
	heartbeat := func(ctx context.Context) error {
		m.heartbeats.Add(1)
		err := task.Heartbeat(ctx)
		if err == nil {
			err = ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			m.heartbeatCancellations.Add(1)
		} else if err != nil {
			m.heartbeatFailures.Add(1)
		}
		return err
	}
	result = s.Run(ctx, heartbeat, task.Execute)
	returned = true
	return result
}
