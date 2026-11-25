package postgres

import (
	"sync"
	"sync/atomic"
	"time"
)

type RollupMetrics struct {
	AggregateSQLTotalNanosByPath map[string]int64  `json:"aggregate_sql_total_nanos_by_path"`
	AggregateSQLCountByPath      map[string]uint64 `json:"aggregate_sql_count_by_path"`
	RollupFallbackReasons        map[string]uint64 `json:"rollup_fallback_reasons"`
	RollupShadowMismatchTotal    uint64            `json:"rollup_shadow_mismatch_total"`
	RollupReconcileMismatchTotal uint64            `json:"rollup_reconcile_mismatch_total"`
	RollupApplierLagSeconds      float64           `json:"rollup_applier_lag_seconds"`
	RollupPendingDeltas          int64             `json:"rollup_pending_deltas"`
	RollupDefinitionsByStatus    map[string]int    `json:"rollup_definitions_by_status"`
}

var (
	rollupMetricsMu            sync.Mutex
	rollupSQLTotalNanosByPath  = map[string]int64{}
	rollupSQLCountByPath       = map[string]uint64{}
	rollupFallbackReasonCounts = map[string]uint64{}

	rollupShadowMismatchTotal    atomic.Uint64
	rollupReconcileMismatchTotal atomic.Uint64
	rollupApplierLagMillis       atomic.Int64
)

func recordAggregateSQLDuration(path string, d time.Duration) {
	rollupMetricsMu.Lock()
	rollupSQLTotalNanosByPath[path] += d.Nanoseconds()
	rollupSQLCountByPath[path]++
	rollupMetricsMu.Unlock()
}

func recordRollupPathFallback(reason string) {
	if reason == "" {
		return
	}
	rollupMetricsMu.Lock()
	rollupFallbackReasonCounts[reason]++
	rollupMetricsMu.Unlock()
}

func recordRollupShadowMismatch() {
	rollupShadowMismatchTotal.Add(1)
}

func recordRollupReconcileMismatch() {
	rollupReconcileMismatchTotal.Add(1)
}

func RecordRollupApplierLag(lag time.Duration) {
	if lag < 0 {
		lag = 0
	}
	rollupApplierLagMillis.Store(lag.Milliseconds())
}

func RollupMetricsSnapshot() RollupMetrics {
	rollupMetricsMu.Lock()
	nanos := make(map[string]int64, len(rollupSQLTotalNanosByPath))
	for k, v := range rollupSQLTotalNanosByPath {
		nanos[k] = v
	}
	counts := make(map[string]uint64, len(rollupSQLCountByPath))
	for k, v := range rollupSQLCountByPath {
		counts[k] = v
	}
	reasons := make(map[string]uint64, len(rollupFallbackReasonCounts))
	for k, v := range rollupFallbackReasonCounts {
		reasons[k] = v
	}
	rollupMetricsMu.Unlock()

	return RollupMetrics{
		AggregateSQLTotalNanosByPath: nanos,
		AggregateSQLCountByPath:      counts,
		RollupFallbackReasons:        reasons,
		RollupShadowMismatchTotal:    rollupShadowMismatchTotal.Load(),
		RollupReconcileMismatchTotal: rollupReconcileMismatchTotal.Load(),
		RollupApplierLagSeconds:      float64(rollupApplierLagMillis.Load()) / 1000.0,
	}
}
