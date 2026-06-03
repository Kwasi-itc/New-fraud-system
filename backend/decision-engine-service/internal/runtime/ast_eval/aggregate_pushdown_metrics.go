package ast_eval

import (
	"sync/atomic"
	"time"
)

type AggregatePushdownMetrics struct {
	CompileSupportedCount   uint64
	CompileUnsupportedCount uint64
	FallbackCount           uint64
	RemoteCallCount         uint64
	RemoteErrorCount        uint64
	RemoteLatencyTotal      time.Duration
}

var aggregatePushdownCompileSupportedCount atomic.Uint64
var aggregatePushdownCompileUnsupportedCount atomic.Uint64
var aggregatePushdownFallbackCount atomic.Uint64
var aggregatePushdownRemoteCallCount atomic.Uint64
var aggregatePushdownRemoteErrorCount atomic.Uint64
var aggregatePushdownRemoteLatencyTotalNanos atomic.Int64

func recordAggregatePushdownCompile(supported bool) {
	if supported {
		aggregatePushdownCompileSupportedCount.Add(1)
		return
	}
	aggregatePushdownCompileUnsupportedCount.Add(1)
}

func recordAggregatePushdownFallback() {
	aggregatePushdownFallbackCount.Add(1)
}

func recordAggregatePushdownRemoteCall(duration time.Duration, err error) {
	aggregatePushdownRemoteCallCount.Add(1)
	aggregatePushdownRemoteLatencyTotalNanos.Add(duration.Nanoseconds())
	if err != nil {
		aggregatePushdownRemoteErrorCount.Add(1)
	}
}

func AggregatePushdownMetricsSnapshot() AggregatePushdownMetrics {
	return AggregatePushdownMetrics{
		CompileSupportedCount:   aggregatePushdownCompileSupportedCount.Load(),
		CompileUnsupportedCount: aggregatePushdownCompileUnsupportedCount.Load(),
		FallbackCount:           aggregatePushdownFallbackCount.Load(),
		RemoteCallCount:         aggregatePushdownRemoteCallCount.Load(),
		RemoteErrorCount:        aggregatePushdownRemoteErrorCount.Load(),
		RemoteLatencyTotal:      time.Duration(aggregatePushdownRemoteLatencyTotalNanos.Load()),
	}
}
