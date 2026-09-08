package tenantdata

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const aggregateFactLatencySampleLimit = 1024

type AggregateFactValkeyLatency struct {
	Requests      uint64 `json:"requests"`
	Failures      uint64 `json:"failures"`
	Buckets       uint64 `json:"buckets"`
	TotalMicros   uint64 `json:"total_micros"`
	AverageMicros uint64 `json:"average_micros"`
	P50Micros     uint64 `json:"p50_micros"`
	P95Micros     uint64 `json:"p95_micros"`
	P99Micros     uint64 `json:"p99_micros"`
	MaxMicros     uint64 `json:"max_micros"`
}

type AggregateFactMetrics struct {
	Requests                   uint64                     `json:"requests"`
	MatchedRequests            uint64                     `json:"matched_requests"`
	FactHits                   uint64                     `json:"fact_hits"`
	FallbackNoMatchingFact     uint64                     `json:"fallback_no_matching_fact"`
	FallbackRegistryFailure    uint64                     `json:"fallback_registry_failure"`
	IncompleteCoverageFailures uint64                     `json:"incomplete_coverage_failures"`
	CoverageReadFailures       uint64                     `json:"coverage_read_failures"`
	BucketReadFailures         uint64                     `json:"bucket_read_failures"`
	PendingBucketFailures      uint64                     `json:"pending_bucket_failures"`
	NoUsableBucketsFailures    uint64                     `json:"no_usable_buckets_failures"`
	MinuteBucketsRead          uint64                     `json:"minute_buckets_read"`
	HourBucketsRead            uint64                     `json:"hour_buckets_read"`
	DayBucketsRead             uint64                     `json:"day_buckets_read"`
	ValkeyCoverageReads        AggregateFactValkeyLatency `json:"valkey_coverage_reads"`
	ValkeyBucketReads          AggregateFactValkeyLatency `json:"valkey_bucket_reads"`
}

type aggregateFactMetricState struct {
	requests                   atomic.Uint64
	matchedRequests            atomic.Uint64
	factHits                   atomic.Uint64
	fallbackNoMatchingFact     atomic.Uint64
	fallbackRegistryFailure    atomic.Uint64
	incompleteCoverageFailures atomic.Uint64
	coverageReadFailures       atomic.Uint64
	bucketReadFailures         atomic.Uint64
	pendingBucketFailures      atomic.Uint64
	noUsableBucketsFailures    atomic.Uint64
	minuteBucketsRead          atomic.Uint64
	hourBucketsRead            atomic.Uint64
	dayBucketsRead             atomic.Uint64
}

var aggregateFactMetrics aggregateFactMetricState
var aggregateFactCoverageReadLatency aggregateFactValkeyLatencyState
var aggregateFactBucketReadLatency aggregateFactValkeyLatencyState

type aggregateFactValkeyLatencyState struct {
	requests    atomic.Uint64
	failures    atomic.Uint64
	buckets     atomic.Uint64
	totalMicros atomic.Uint64
	maxMicros   atomic.Uint64
	mu          sync.Mutex
	samples     []uint64
}

func (s *aggregateFactValkeyLatencyState) record(duration time.Duration, err error, buckets int) {
	micros := uint64(max(duration.Microseconds(), 0))
	s.requests.Add(1)
	if err != nil {
		s.failures.Add(1)
	}
	if buckets > 0 {
		s.buckets.Add(uint64(buckets))
	}
	s.totalMicros.Add(micros)
	for current := s.maxMicros.Load(); micros > current && !s.maxMicros.CompareAndSwap(current, micros); current = s.maxMicros.Load() {
	}
	s.mu.Lock()
	if len(s.samples) >= aggregateFactLatencySampleLimit {
		copy(s.samples, s.samples[1:])
		s.samples[len(s.samples)-1] = micros
	} else {
		s.samples = append(s.samples, micros)
	}
	s.mu.Unlock()
}

func (s *aggregateFactValkeyLatencyState) snapshot() AggregateFactValkeyLatency {
	requests := s.requests.Load()
	total := s.totalMicros.Load()
	s.mu.Lock()
	samples := append([]uint64(nil), s.samples...)
	s.mu.Unlock()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return AggregateFactValkeyLatency{
		Requests: requests, Failures: s.failures.Load(), Buckets: s.buckets.Load(), TotalMicros: total,
		AverageMicros: averageMicros(total, requests), P50Micros: percentileMicros(samples, 50),
		P95Micros: percentileMicros(samples, 95), P99Micros: percentileMicros(samples, 99), MaxMicros: s.maxMicros.Load(),
	}
}

func recordAggregateFactValkeyRead(operation string, duration time.Duration, err error, buckets int) {
	switch operation {
	case "coverage":
		aggregateFactCoverageReadLatency.record(duration, err, buckets)
	case "buckets":
		aggregateFactBucketReadLatency.record(duration, err, buckets)
	}
}

func averageMicros(total, count uint64) uint64 {
	if count == 0 {
		return 0
	}
	return total / count
}

func percentileMicros(samples []uint64, percentile int) uint64 {
	if len(samples) == 0 {
		return 0
	}
	index := ((len(samples) - 1) * percentile) / 100
	return samples[index]
}

func AggregateFactMetricsSnapshot() AggregateFactMetrics {
	return AggregateFactMetrics{
		Requests: aggregateFactMetrics.requests.Load(), MatchedRequests: aggregateFactMetrics.matchedRequests.Load(),
		FactHits:                   aggregateFactMetrics.factHits.Load(),
		FallbackNoMatchingFact:     aggregateFactMetrics.fallbackNoMatchingFact.Load(),
		FallbackRegistryFailure:    aggregateFactMetrics.fallbackRegistryFailure.Load(),
		IncompleteCoverageFailures: aggregateFactMetrics.incompleteCoverageFailures.Load(),
		CoverageReadFailures:       aggregateFactMetrics.coverageReadFailures.Load(),
		BucketReadFailures:         aggregateFactMetrics.bucketReadFailures.Load(),
		PendingBucketFailures:      aggregateFactMetrics.pendingBucketFailures.Load(),
		NoUsableBucketsFailures:    aggregateFactMetrics.noUsableBucketsFailures.Load(),
		MinuteBucketsRead:          aggregateFactMetrics.minuteBucketsRead.Load(),
		HourBucketsRead:            aggregateFactMetrics.hourBucketsRead.Load(),
		DayBucketsRead:             aggregateFactMetrics.dayBucketsRead.Load(),
		ValkeyCoverageReads:        aggregateFactCoverageReadLatency.snapshot(),
		ValkeyBucketReads:          aggregateFactBucketReadLatency.snapshot(),
	}
}
