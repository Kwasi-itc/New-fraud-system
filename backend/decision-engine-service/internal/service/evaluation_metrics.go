package service

import (
	"slices"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/evalerrors"
)

const latencySampleLimit = 512

type DecisionEvaluationMetrics struct {
	SingleScenario operationMetricsSnapshot `json:"single_scenario"`
	MultiScenario  operationMetricsSnapshot `json:"multi_scenario"`
}

type operationMetricsSnapshot struct {
	Requests           uint64            `json:"requests"`
	Successes          uint64            `json:"successes"`
	Failures           uint64            `json:"failures"`
	Triggered          uint64            `json:"triggered"`
	NotTriggered       uint64            `json:"not_triggered"`
	TotalLatencyMicros int64             `json:"total_latency_micros"`
	LastLatencyMicros  int64             `json:"last_latency_micros"`
	P50LatencyMicros   int64             `json:"p50_latency_micros"`
	P95LatencyMicros   int64             `json:"p95_latency_micros"`
	P99LatencyMicros   int64             `json:"p99_latency_micros"`
	FailureCategories  map[string]uint64 `json:"failure_categories"`
	LastErrorCategory  string            `json:"last_error_category,omitempty"`
}

type evaluationMetricsCollector struct {
	mu             sync.Mutex
	singleScenario operationMetricsState
	multiScenario  operationMetricsState
}

type operationMetricsState struct {
	Requests           uint64
	Successes          uint64
	Failures           uint64
	Triggered          uint64
	NotTriggered       uint64
	TotalLatencyMicros int64
	LastLatencyMicros  int64
	LastErrorCategory  string
	FailureCategories  map[string]uint64
	LatencySamples     []int64
}

func newEvaluationMetricsCollector() *evaluationMetricsCollector {
	return &evaluationMetricsCollector{
		singleScenario: newOperationMetricsState(),
		multiScenario:  newOperationMetricsState(),
	}
}

func newOperationMetricsState() operationMetricsState {
	return operationMetricsState{
		FailureCategories: map[string]uint64{},
		LatencySamples:    make([]int64, 0, latencySampleLimit),
	}
}

func (c *evaluationMetricsCollector) recordSingle(duration time.Duration, triggered bool, err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	recordOperationMetric(&c.singleScenario, duration, triggered, err)
}

func (c *evaluationMetricsCollector) recordMulti(duration time.Duration, resultCount int, err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	recordOperationMetric(&c.multiScenario, duration, resultCount > 0, err)
}

func (c *evaluationMetricsCollector) snapshot() DecisionEvaluationMetrics {
	if c == nil {
		return DecisionEvaluationMetrics{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return DecisionEvaluationMetrics{
		SingleScenario: snapshotOperationMetrics(c.singleScenario),
		MultiScenario:  snapshotOperationMetrics(c.multiScenario),
	}
}

func recordOperationMetric(state *operationMetricsState, duration time.Duration, triggered bool, err error) {
	state.Requests++
	latencyMicros := duration.Microseconds()
	state.TotalLatencyMicros += latencyMicros
	state.LastLatencyMicros = latencyMicros
	recordLatencySample(state, latencyMicros)
	if err != nil {
		state.Failures++
		category := evalerrors.Classify(err).Category
		state.FailureCategories[category]++
		state.LastErrorCategory = category
		return
	}
	state.Successes++
	state.LastErrorCategory = ""
	if triggered {
		state.Triggered++
	} else {
		state.NotTriggered++
	}
}

func recordLatencySample(state *operationMetricsState, latencyMicros int64) {
	if len(state.LatencySamples) >= latencySampleLimit {
		copy(state.LatencySamples, state.LatencySamples[1:])
		state.LatencySamples[len(state.LatencySamples)-1] = latencyMicros
		return
	}
	state.LatencySamples = append(state.LatencySamples, latencyMicros)
}

func snapshotOperationMetrics(state operationMetricsState) operationMetricsSnapshot {
	snapshot := operationMetricsSnapshot{
		Requests:           state.Requests,
		Successes:          state.Successes,
		Failures:           state.Failures,
		Triggered:          state.Triggered,
		NotTriggered:       state.NotTriggered,
		TotalLatencyMicros: state.TotalLatencyMicros,
		LastLatencyMicros:  state.LastLatencyMicros,
		LastErrorCategory:  state.LastErrorCategory,
		FailureCategories:  make(map[string]uint64, len(state.FailureCategories)),
	}
	for key, value := range state.FailureCategories {
		snapshot.FailureCategories[key] = value
	}
	snapshot.P50LatencyMicros = percentileMicros(state.LatencySamples, 50)
	snapshot.P95LatencyMicros = percentileMicros(state.LatencySamples, 95)
	snapshot.P99LatencyMicros = percentileMicros(state.LatencySamples, 99)
	return snapshot
}

func percentileMicros(samples []int64, percentile int) int64 {
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
	return sorted[index]
}
