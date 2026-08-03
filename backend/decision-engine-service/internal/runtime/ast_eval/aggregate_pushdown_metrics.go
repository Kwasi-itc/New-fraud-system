package ast_eval

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const aggregateLatencySampleLimit = 512

type AggregatePushdownMetrics struct {
	CompileSupportedCount            uint64
	CompileUnsupportedCount          uint64
	FallbackCount                    uint64
	FallbackAggregateNotEnabledCount uint64
	FallbackRemoteCallFailedCount    uint64
	FallbackUnsupportedQueryCount    uint64
	AggregateEvaluationCount         uint64
	RemoteCallCount                  uint64
	RemoteErrorCount                 uint64
	AggregateLatencyTotal            time.Duration
	AggregateLatencyP50              time.Duration
	AggregateLatencyP95              time.Duration
	AggregateLatencyP99              time.Duration
	RemoteLatencyTotal               time.Duration
}

var aggregatePushdownCompileSupportedCount atomic.Uint64
var aggregatePushdownCompileUnsupportedCount atomic.Uint64
var aggregatePushdownFallbackCount atomic.Uint64
var aggregatePushdownFallbackAggregateNotEnabledCount atomic.Uint64
var aggregatePushdownFallbackRemoteCallFailedCount atomic.Uint64
var aggregatePushdownFallbackUnsupportedQueryCount atomic.Uint64
var aggregatePushdownEvaluationCount atomic.Uint64
var aggregatePushdownRemoteCallCount atomic.Uint64
var aggregatePushdownRemoteErrorCount atomic.Uint64
var aggregatePushdownEvaluationLatencyTotalNanos atomic.Int64
var aggregatePushdownRemoteLatencyTotalNanos atomic.Int64
var aggregatePushdownLatencySamplesMu sync.Mutex
var aggregatePushdownLatencySamples = make([]int64, 0, aggregateLatencySampleLimit)

func recordAggregatePushdownCompile(supported bool) {
	if supported {
		aggregatePushdownCompileSupportedCount.Add(1)
		return
	}
	aggregatePushdownCompileUnsupportedCount.Add(1)
}

func recordAggregatePushdownFallback(reason string) {
	aggregatePushdownFallbackCount.Add(1)
	switch reason {
	case "aggregate_not_enabled":
		aggregatePushdownFallbackAggregateNotEnabledCount.Add(1)
	case "remote_call_failed":
		aggregatePushdownFallbackRemoteCallFailedCount.Add(1)
	case "unsupported_query_shape":
		aggregatePushdownFallbackUnsupportedQueryCount.Add(1)
	}
}

func recordAggregateEvaluation(duration time.Duration) {
	aggregatePushdownEvaluationCount.Add(1)
	aggregatePushdownEvaluationLatencyTotalNanos.Add(duration.Nanoseconds())
	aggregatePushdownLatencySamplesMu.Lock()
	defer aggregatePushdownLatencySamplesMu.Unlock()
	latencyNanos := duration.Nanoseconds()
	if len(aggregatePushdownLatencySamples) >= aggregateLatencySampleLimit {
		copy(aggregatePushdownLatencySamples, aggregatePushdownLatencySamples[1:])
		aggregatePushdownLatencySamples[len(aggregatePushdownLatencySamples)-1] = latencyNanos
		return
	}
	aggregatePushdownLatencySamples = append(aggregatePushdownLatencySamples, latencyNanos)
}

func recordAggregatePushdownRemoteCall(duration time.Duration, err error) {
	aggregatePushdownRemoteCallCount.Add(1)
	aggregatePushdownRemoteLatencyTotalNanos.Add(duration.Nanoseconds())
	if err != nil {
		aggregatePushdownRemoteErrorCount.Add(1)
	}
}

func AggregatePushdownMetricsSnapshot() AggregatePushdownMetrics {
	aggregatePushdownLatencySamplesMu.Lock()
	samples := append([]int64(nil), aggregatePushdownLatencySamples...)
	aggregatePushdownLatencySamplesMu.Unlock()
	return AggregatePushdownMetrics{
		CompileSupportedCount:            aggregatePushdownCompileSupportedCount.Load(),
		CompileUnsupportedCount:          aggregatePushdownCompileUnsupportedCount.Load(),
		FallbackCount:                    aggregatePushdownFallbackCount.Load(),
		FallbackAggregateNotEnabledCount: aggregatePushdownFallbackAggregateNotEnabledCount.Load(),
		FallbackRemoteCallFailedCount:    aggregatePushdownFallbackRemoteCallFailedCount.Load(),
		FallbackUnsupportedQueryCount:    aggregatePushdownFallbackUnsupportedQueryCount.Load(),
		AggregateEvaluationCount:         aggregatePushdownEvaluationCount.Load(),
		RemoteCallCount:                  aggregatePushdownRemoteCallCount.Load(),
		RemoteErrorCount:                 aggregatePushdownRemoteErrorCount.Load(),
		AggregateLatencyTotal:            time.Duration(aggregatePushdownEvaluationLatencyTotalNanos.Load()),
		AggregateLatencyP50:              percentileDuration(samples, 50),
		AggregateLatencyP95:              percentileDuration(samples, 95),
		AggregateLatencyP99:              percentileDuration(samples, 99),
		RemoteLatencyTotal:               time.Duration(aggregatePushdownRemoteLatencyTotalNanos.Load()),
	}
}

func percentileDuration(samples []int64, percentile int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]int64(nil), samples...)
	slices.Sort(sorted)
	index := ((len(sorted) - 1) * percentile) / 100
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return time.Duration(sorted[index])
}
