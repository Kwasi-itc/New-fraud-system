package ast_eval

import (
	"context"
	"slices"
	"strings"
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
	RemoteLimiterWaitCount           uint64
	RemoteLimiterWaitTotal           time.Duration
	RemoteConcurrencyLimit           int
	TopRules                         map[string]AggregateRulePressure
	TopShapes                        map[string]uint64
}

type AggregateRulePressure struct {
	RemoteCallCount uint64 `json:"remote_call_count"`
	RemoteErrorCount uint64 `json:"remote_error_count"`
	OverloadCount   uint64 `json:"overload_count"`
	LastAggregate   string `json:"last_aggregate,omitempty"`
	LastField       string `json:"last_field,omitempty"`
	LastShape       string `json:"last_shape,omitempty"`
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
var aggregatePushdownRemoteLimiterWaitCount atomic.Uint64
var aggregatePushdownRemoteLimiterWaitTotalNanos atomic.Int64
var aggregatePushdownLatencySamplesMu sync.Mutex
var aggregatePushdownLatencySamples = make([]int64, 0, aggregateLatencySampleLimit)
var aggregatePushdownRuleMetricsMu sync.Mutex
var aggregatePushdownRuleMetrics = map[string]*AggregateRulePressure{}
var aggregatePushdownShapeCounts = map[string]uint64{}
var aggregateRemoteLimiterState atomic.Int64

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

func recordAggregateRemoteLimiterWait(duration time.Duration) {
	if duration <= 0 {
		return
	}
	aggregatePushdownRemoteLimiterWaitCount.Add(1)
	aggregatePushdownRemoteLimiterWaitTotalNanos.Add(duration.Nanoseconds())
}

func setAggregateRemoteConcurrencyLimit(limit int) {
	aggregateRemoteLimiterState.Store(int64(limit))
}

func recordAggregatePressure(ctx context.Context, queryShape string, aggregate string, field string, err error) {
	ruleName := RuleNameFromContext(ctx)
	aggregatePushdownRuleMetricsMu.Lock()
	defer aggregatePushdownRuleMetricsMu.Unlock()
	if queryShape != "" {
		aggregatePushdownShapeCounts[queryShape]++
	}
	if ruleName == "" {
		return
	}
	item, ok := aggregatePushdownRuleMetrics[ruleName]
	if !ok {
		item = &AggregateRulePressure{}
		aggregatePushdownRuleMetrics[ruleName] = item
	}
	item.RemoteCallCount++
	item.LastAggregate = aggregate
	item.LastField = field
	item.LastShape = queryShape
	if err != nil {
		item.RemoteErrorCount++
		if isAggregateOverloadError(err) {
			item.OverloadCount++
		}
	}
}

func AggregatePushdownMetricsSnapshot() AggregatePushdownMetrics {
	aggregatePushdownLatencySamplesMu.Lock()
	samples := append([]int64(nil), aggregatePushdownLatencySamples...)
	aggregatePushdownLatencySamplesMu.Unlock()
	aggregatePushdownRuleMetricsMu.Lock()
	ruleMetrics := make(map[string]AggregateRulePressure, len(aggregatePushdownRuleMetrics))
	for key, value := range aggregatePushdownRuleMetrics {
		ruleMetrics[key] = *value
	}
	shapeCounts := make(map[string]uint64, len(aggregatePushdownShapeCounts))
	for key, value := range aggregatePushdownShapeCounts {
		shapeCounts[key] = value
	}
	aggregatePushdownRuleMetricsMu.Unlock()
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
		RemoteLimiterWaitCount:           aggregatePushdownRemoteLimiterWaitCount.Load(),
		RemoteLimiterWaitTotal:           time.Duration(aggregatePushdownRemoteLimiterWaitTotalNanos.Load()),
		RemoteConcurrencyLimit:           int(aggregateRemoteLimiterState.Load()),
		TopRules:                         ruleMetrics,
		TopShapes:                        shapeCounts,
	}
}

func isAggregateOverloadError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unexpected status from ingestion-service: 429")
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
