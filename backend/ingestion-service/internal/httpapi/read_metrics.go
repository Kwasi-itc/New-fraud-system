package httpapi

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DBPoolStats struct {
	AcquireCount           int64 `json:"acquire_count"`
	AcquireDurationMicros  int64 `json:"acquire_duration_micros"`
	EmptyAcquireCount      int64 `json:"empty_acquire_count"`
	EmptyAcquireWaitMicros int64 `json:"empty_acquire_wait_micros"`
	CanceledAcquireCount   int64 `json:"canceled_acquire_count"`
	MaxConns               int32 `json:"max_conns"`
	TotalConns             int32 `json:"total_conns"`
	AcquiredConns          int32 `json:"acquired_conns"`
	IdleConns              int32 `json:"idle_conns"`
	ConstructingConns      int32 `json:"constructing_conns"`
}

type dbPoolStatsProvider func() DBPoolStats

type OverloadThresholds struct {
	DBPoolSaturationPct    int `json:"db_pool_saturation_pct"`
	RequestQueueDepth      int `json:"request_queue_depth"`
	ServiceCPUPercent      int `json:"service_cpu_percent"`
	UpstreamTimeoutRatePct int `json:"upstream_timeout_rate_pct"`
}

const latencySampleLimit = 512

type readMetricsCollector struct {
	mu         sync.Mutex
	endpoints  map[string]*endpointMetrics
	provider   dbPoolStatsProvider
	thresholds OverloadThresholds
}

type endpointMetrics struct {
	Requests             int64            `json:"requests"`
	InFlight             int64            `json:"in_flight"`
	Successes            int64            `json:"successes"`
	Overloads            int64            `json:"overloads"`
	Timeouts             int64            `json:"timeouts"`
	Cancellations        int64            `json:"cancellations"`
	DependencyFailures   int64            `json:"dependency_failures"`
	ValidationFailures   int64            `json:"validation_failures"`
	InternalFailures     int64            `json:"internal_failures"`
	TotalLatencyMicros   int64            `json:"total_latency_micros"`
	LastLatencyMicros    int64            `json:"last_latency_micros"`
	P50LatencyMicros     int64            `json:"p50_latency_micros"`
	P95LatencyMicros     int64            `json:"p95_latency_micros"`
	P99LatencyMicros     int64            `json:"p99_latency_micros"`
	StatusCounts         map[int]int64    `json:"status_counts"`
	AggregateCounts      map[string]int64 `json:"aggregate_counts,omitempty"`
	AggregateShapeCounts map[string]int64 `json:"aggregate_shape_counts,omitempty"`
	ObjectTypeCounts     map[string]int64 `json:"object_type_counts,omitempty"`
	FilterDepthCounts    map[int]int64    `json:"filter_depth_counts,omitempty"`
	ListLimitCounts      map[int]int64    `json:"list_limit_counts,omitempty"`
	LastErrorCategory    string           `json:"last_error_category,omitempty"`
	LastAggregate        string           `json:"last_aggregate,omitempty"`
	LastAggregateShape   string           `json:"last_aggregate_shape,omitempty"`
	LastObjectType       string           `json:"last_object_type,omitempty"`
	LastFilterDepth      int              `json:"last_filter_depth,omitempty"`
	LastListLimit        int              `json:"last_list_limit,omitempty"`
	MaxListLimit         int              `json:"max_list_limit,omitempty"`
	LatencySamplesMicros []int64          `json:"-"`
}

type readMetricsSnapshot struct {
	Endpoints  map[string]endpointMetrics `json:"endpoints"`
	DBPool     *DBPoolStats               `json:"db_pool,omitempty"`
	Thresholds OverloadThresholds         `json:"thresholds"`
	Pressure   readMetricsPressure        `json:"pressure"`
}

type readMetricsPressure struct {
	Status                  string   `json:"status"`
	Reasons                 []string `json:"reasons,omitempty"`
	DBPoolSaturationPct     int      `json:"db_pool_saturation_pct"`
	ActiveReadRequests      int64    `json:"active_read_requests"`
	AggregateTimeoutRatePct int      `json:"aggregate_timeout_rate_pct"`
	ReadOverloadCount       int64    `json:"read_overload_count"`
	AggregateOverloadCount  int64    `json:"aggregate_overload_count"`
}

func newReadMetricsCollector(provider dbPoolStatsProvider) *readMetricsCollector {
	return &readMetricsCollector{
		endpoints: map[string]*endpointMetrics{},
		provider:  provider,
	}
}

func (c *readMetricsCollector) SetThresholds(thresholds OverloadThresholds) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.thresholds = thresholds
}

func (c *readMetricsCollector) middleware(endpoint string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		startedAt := time.Now()
		c.begin(endpoint)
		defer func() {
			aggregate, _ := ctx.Get("metric_aggregate")
			aggregateShape, _ := ctx.Get("metric_aggregate_shape")
			objectType, _ := ctx.Get("metric_object_type")
			filterDepth, _ := ctx.Get("metric_filter_depth")
			listLimit, _ := ctx.Get("metric_list_limit")
			errorCategory, _ := ctx.Get("error_category")
			c.finish(endpoint, ctx.Writer.Status(), time.Since(startedAt), stringify(aggregate), stringify(aggregateShape), stringify(objectType), intValue(filterDepth), intValue(listLimit), stringify(errorCategory))
		}()
		ctx.Next()
	}
}

func (c *readMetricsCollector) begin(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.endpoint(endpoint)
	item.Requests++
	item.InFlight++
}

func (c *readMetricsCollector) finish(endpoint string, status int, duration time.Duration, aggregate, aggregateShape, objectType string, filterDepth int, listLimit int, errorCategory string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.endpoint(endpoint)
	if item.InFlight > 0 {
		item.InFlight--
	}
	item.StatusCounts[status]++
	item.TotalLatencyMicros += duration.Microseconds()
	item.LastLatencyMicros = duration.Microseconds()
	recordLatencySample(&item.LatencySamplesMicros, item.LastLatencyMicros)
	if aggregate != "" {
		item.AggregateCounts[aggregate]++
		item.LastAggregate = aggregate
	}
	if aggregateShape != "" {
		item.AggregateShapeCounts[aggregateShape]++
		item.LastAggregateShape = aggregateShape
	}
	if objectType != "" {
		item.ObjectTypeCounts[objectType]++
		item.LastObjectType = objectType
	}
	if filterDepth > 0 {
		item.FilterDepthCounts[filterDepth]++
		item.LastFilterDepth = filterDepth
	}
	if listLimit > 0 {
		item.ListLimitCounts[listLimit]++
		item.LastListLimit = listLimit
		if listLimit > item.MaxListLimit {
			item.MaxListLimit = listLimit
		}
	}
	switch {
	case status >= 200 && status < 300:
		item.Successes++
	case status == 429:
		item.Overloads++
	case errorCategory == "timeout":
		item.Timeouts++
	case errorCategory == "canceled":
		item.Cancellations++
	case errorCategory == "dependency_failure":
		item.DependencyFailures++
	case errorCategory == "invalid_request" || errorCategory == "validation_failed":
		item.ValidationFailures++
	case status >= 500:
		item.InternalFailures++
	}
	if errorCategory != "" {
		item.LastErrorCategory = errorCategory
	}
}

func (c *readMetricsCollector) snapshot() readMetricsSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	endpoints := make(map[string]endpointMetrics, len(c.endpoints))
	for key, value := range c.endpoints {
		copyValue := endpointMetrics{
			Requests:             value.Requests,
			InFlight:             value.InFlight,
			Successes:            value.Successes,
			Overloads:            value.Overloads,
			Timeouts:             value.Timeouts,
			Cancellations:        value.Cancellations,
			DependencyFailures:   value.DependencyFailures,
			ValidationFailures:   value.ValidationFailures,
			InternalFailures:     value.InternalFailures,
			TotalLatencyMicros:   value.TotalLatencyMicros,
			LastLatencyMicros:    value.LastLatencyMicros,
			LastErrorCategory:    value.LastErrorCategory,
			LastAggregate:        value.LastAggregate,
			LastAggregateShape:   value.LastAggregateShape,
			LastObjectType:       value.LastObjectType,
			LastFilterDepth:      value.LastFilterDepth,
			LastListLimit:        value.LastListLimit,
			MaxListLimit:         value.MaxListLimit,
			StatusCounts:         make(map[int]int64, len(value.StatusCounts)),
			AggregateCounts:      make(map[string]int64, len(value.AggregateCounts)),
			AggregateShapeCounts: make(map[string]int64, len(value.AggregateShapeCounts)),
			ObjectTypeCounts:     make(map[string]int64, len(value.ObjectTypeCounts)),
			FilterDepthCounts:    make(map[int]int64, len(value.FilterDepthCounts)),
			ListLimitCounts:      make(map[int]int64, len(value.ListLimitCounts)),
		}
		copyValue.P50LatencyMicros = percentileMicros(value.LatencySamplesMicros, 50)
		copyValue.P95LatencyMicros = percentileMicros(value.LatencySamplesMicros, 95)
		copyValue.P99LatencyMicros = percentileMicros(value.LatencySamplesMicros, 99)
		for status, count := range value.StatusCounts {
			copyValue.StatusCounts[status] = count
		}
		for aggregate, count := range value.AggregateCounts {
			copyValue.AggregateCounts[aggregate] = count
		}
		for shape, count := range value.AggregateShapeCounts {
			copyValue.AggregateShapeCounts[shape] = count
		}
		for objectType, count := range value.ObjectTypeCounts {
			copyValue.ObjectTypeCounts[objectType] = count
		}
		for filterDepth, count := range value.FilterDepthCounts {
			copyValue.FilterDepthCounts[filterDepth] = count
		}
		for listLimit, count := range value.ListLimitCounts {
			copyValue.ListLimitCounts[listLimit] = count
		}
		endpoints[key] = copyValue
	}

	snapshot := readMetricsSnapshot{
		Endpoints:  endpoints,
		Thresholds: c.thresholds,
	}
	if c.provider != nil {
		stats := c.provider()
		snapshot.DBPool = &stats
	}
	snapshot.Pressure = buildReadMetricsPressure(snapshot)
	return snapshot
}

func (c *readMetricsCollector) Snapshot() any {
	return c.snapshot()
}

func (c *readMetricsCollector) endpoint(name string) *endpointMetrics {
	item, ok := c.endpoints[name]
	if ok {
		return item
	}
	item = &endpointMetrics{
		StatusCounts:         map[int]int64{},
		AggregateCounts:      map[string]int64{},
		AggregateShapeCounts: map[string]int64{},
		ObjectTypeCounts:     map[string]int64{},
		FilterDepthCounts:    map[int]int64{},
		ListLimitCounts:      map[int]int64{},
		LatencySamplesMicros: make([]int64, 0, latencySampleLimit),
	}
	c.endpoints[name] = item
	return item
}

func dbPoolStatsFromPool(db *pgxpool.Pool) dbPoolStatsProvider {
	if db == nil {
		return nil
	}
	return func() DBPoolStats {
		stats := db.Stat()
		return DBPoolStats{
			AcquireCount:           stats.AcquireCount(),
			AcquireDurationMicros:  stats.AcquireDuration().Microseconds(),
			EmptyAcquireCount:      stats.EmptyAcquireCount(),
			EmptyAcquireWaitMicros: stats.EmptyAcquireWaitTime().Microseconds(),
			CanceledAcquireCount:   stats.CanceledAcquireCount(),
			MaxConns:               stats.MaxConns(),
			TotalConns:             stats.TotalConns(),
			AcquiredConns:          stats.AcquiredConns(),
			IdleConns:              stats.IdleConns(),
			ConstructingConns:      stats.ConstructingConns(),
		}
	}
}

func stringify(value any) string {
	out, _ := value.(string)
	return out
}

func intValue(value any) int {
	out, _ := value.(int)
	return out
}

func recordLatencySample(samples *[]int64, latencyMicros int64) {
	if len(*samples) >= latencySampleLimit {
		copy((*samples), (*samples)[1:])
		(*samples)[len(*samples)-1] = latencyMicros
		return
	}
	*samples = append(*samples, latencyMicros)
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

func buildReadMetricsPressure(snapshot readMetricsSnapshot) readMetricsPressure {
	pressure := readMetricsPressure{Status: "ok"}
	if snapshot.DBPool != nil && snapshot.DBPool.MaxConns > 0 {
		pressure.DBPoolSaturationPct = int((int64(snapshot.DBPool.AcquiredConns) * 100) / int64(snapshot.DBPool.MaxConns))
	}
	for _, endpoint := range snapshot.Endpoints {
		pressure.ActiveReadRequests += endpoint.InFlight
	}
	if aggregate, ok := snapshot.Endpoints["aggregate_records"]; ok {
		pressure.AggregateOverloadCount = aggregate.Overloads
		if aggregate.Requests > 0 {
			pressure.AggregateTimeoutRatePct = int(((aggregate.Timeouts + aggregate.Cancellations) * 100) / aggregate.Requests)
		}
	}
	for name, endpoint := range snapshot.Endpoints {
		if name == "aggregate_records" {
			continue
		}
		pressure.ReadOverloadCount += endpoint.Overloads
	}

	if threshold := snapshot.Thresholds.DBPoolSaturationPct; threshold > 0 && pressure.DBPoolSaturationPct >= threshold {
		pressure.Reasons = append(pressure.Reasons, fmt.Sprintf("db_pool_saturation>=%d%%", threshold))
	}
	if threshold := snapshot.Thresholds.RequestQueueDepth; threshold > 0 && pressure.ActiveReadRequests >= int64(threshold) {
		pressure.Reasons = append(pressure.Reasons, fmt.Sprintf("active_read_requests>=%d", threshold))
	}
	if threshold := snapshot.Thresholds.UpstreamTimeoutRatePct; threshold > 0 && pressure.AggregateTimeoutRatePct >= threshold {
		pressure.Reasons = append(pressure.Reasons, fmt.Sprintf("aggregate_timeout_rate>=%d%%", threshold))
	}
	if pressure.ReadOverloadCount > 0 || pressure.AggregateOverloadCount > 0 {
		pressure.Reasons = append(pressure.Reasons, "overload_responses_observed")
	}

	switch len(pressure.Reasons) {
	case 0:
		pressure.Status = "ok"
	case 1:
		pressure.Status = "warning"
	default:
		pressure.Status = "critical"
	}
	return pressure
}
