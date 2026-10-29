package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"time"
)

// PoolMetrics reports process-local counters without SQL, credentials, SQL text,
// resource identifiers or tenant labels. Compare snapshots of the SAME pool;
// counters reset when that process/pool is replaced.
type PoolMetrics struct {
	Maximum           int   `json:"maximum"`
	Open              int   `json:"open"`
	InUse             int   `json:"in_use"`
	Idle              int   `json:"idle"`
	WaitCount         int64 `json:"wait_count"`
	WaitMicros        int64 `json:"wait_micros"`
	MaxIdleClosed     int64 `json:"max_idle_closed"`
	MaxIdleTimeClosed int64 `json:"max_idle_time_closed"`
	MaxLifetimeClosed int64 `json:"max_lifetime_closed"`
}

func ObservePool(db *sql.DB) (PoolMetrics, error) {
	if db == nil {
		return PoolMetrics{}, errors.New("database pool required")
	}
	s := db.Stats()
	return PoolMetrics{s.MaxOpenConnections, s.OpenConnections, s.InUse, s.Idle,
		s.WaitCount, s.WaitDuration.Microseconds(), s.MaxIdleClosed, s.MaxIdleTimeClosed, s.MaxLifetimeClosed}, nil
}

// TransactionMetrics is optional local instrumentation. It is concurrency safe,
// has no callbacks that can change commit outcomes, and never retries work.
// Concurrent snapshots are approximate multi-counter observations; reconcile
// totals only after workloads have quiesced. Do not copy a used metrics value.
type TransactionMetrics struct {
	started, active, completed, failed, uncertain atomic.Int64
	elapsedMicros                                 atomic.Int64
}

type TransactionMetricsSnapshot struct {
	Started       int64 `json:"started"`
	Active        int64 `json:"active"`
	Completed     int64 `json:"completed"`
	Failed        int64 `json:"failed"`
	Uncertain     int64 `json:"uncertain"`
	ElapsedMicros int64 `json:"elapsed_micros"`
}

func (m *TransactionMetrics) Snapshot() TransactionMetricsSnapshot {
	if m == nil {
		return TransactionMetricsSnapshot{}
	}
	return TransactionMetricsSnapshot{m.started.Load(), m.active.Load(), m.completed.Load(), m.failed.Load(), m.uncertain.Load(), m.elapsedMicros.Load()}
}

// Mutation/Snapshot elapsed time includes pool acquisition, callback and cleanup.
// Unknown commit/rollback is separate from ordinary failures. Panics still
// propagate after cleanup and are counted as failures, never successes.
func (m *TransactionMetrics) Mutation(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	return m.measure(func() error { return Mutation(ctx, db, fn) })
}

func (m *TransactionMetrics) ReadSnapshot(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	return m.measure(func() error { return Snapshot(ctx, db, fn) })
}

func (m *TransactionMetrics) measure(fn func() error) (result error) {
	if m == nil {
		return errors.New("transaction metrics required")
	}
	started := time.Now()
	m.started.Add(1)
	m.active.Add(1)
	returned := false
	defer func() {
		m.active.Add(-1)
		m.elapsedMicros.Add(time.Since(started).Microseconds())
		var commit *CommitError
		var rollback *RollbackError
		if errors.As(result, &commit) || errors.As(result, &rollback) {
			m.uncertain.Add(1)
		} else if !returned || result != nil {
			m.failed.Add(1)
		} else {
			m.completed.Add(1)
		}
	}()
	result = fn()
	returned = true
	return result
}
