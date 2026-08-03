package httpapi

import (
	"testing"
	"time"
)

func TestReadMetricsCollectorSnapshotIncludesPercentilesAndCancellation(t *testing.T) {
	t.Parallel()

	collector := newReadMetricsCollector(nil)
	collector.begin("aggregate_records")
	collector.finish("aggregate_records", 408, 2*time.Millisecond, "count", "transactions|count|amount|group:and()", "transactions", 2, 0, "canceled")
	collector.begin("aggregate_records")
	collector.finish("aggregate_records", 200, 5*time.Millisecond, "count", "transactions|count|amount|group:and()", "transactions", 3, 0, "")
	collector.begin("aggregate_records")
	collector.finish("aggregate_records", 504, 9*time.Millisecond, "count", "transactions|count|amount|group:and()", "transactions", 3, 0, "timeout")

	snapshot := collector.snapshot()
	endpoint := snapshot.Endpoints["aggregate_records"]
	if endpoint.Cancellations != 1 {
		t.Fatalf("cancellations = %d, want 1", endpoint.Cancellations)
	}
	if endpoint.Timeouts != 1 {
		t.Fatalf("timeouts = %d, want 1", endpoint.Timeouts)
	}
	if endpoint.P50LatencyMicros == 0 || endpoint.P95LatencyMicros == 0 || endpoint.P99LatencyMicros == 0 {
		t.Fatalf("expected non-zero latency percentiles, got %+v", endpoint)
	}
	if endpoint.FilterDepthCounts[3] != 2 {
		t.Fatalf("filter depth count for 3 = %d, want 2", endpoint.FilterDepthCounts[3])
	}
	if endpoint.AggregateShapeCounts["transactions|count|amount|group:and()"] != 3 {
		t.Fatalf("aggregate shape count = %d, want 3", endpoint.AggregateShapeCounts["transactions|count|amount|group:and()"])
	}
}

func TestReadMetricsCollectorSnapshotIncludesThresholds(t *testing.T) {
	t.Parallel()

	collector := newReadMetricsCollector(nil)
	collector.SetThresholds(OverloadThresholds{
		DBPoolSaturationPct:    80,
		RequestQueueDepth:      8,
		ServiceCPUPercent:      85,
		UpstreamTimeoutRatePct: 5,
	})

	snapshot := collector.snapshot()
	if snapshot.Thresholds.DBPoolSaturationPct != 80 {
		t.Fatalf("db pool saturation threshold = %d, want 80", snapshot.Thresholds.DBPoolSaturationPct)
	}
	if snapshot.Thresholds.RequestQueueDepth != 8 {
		t.Fatalf("request queue depth threshold = %d, want 8", snapshot.Thresholds.RequestQueueDepth)
	}
	if snapshot.Thresholds.ServiceCPUPercent != 85 {
		t.Fatalf("service cpu threshold = %d, want 85", snapshot.Thresholds.ServiceCPUPercent)
	}
	if snapshot.Thresholds.UpstreamTimeoutRatePct != 5 {
		t.Fatalf("upstream timeout rate threshold = %d, want 5", snapshot.Thresholds.UpstreamTimeoutRatePct)
	}
}

func TestReadMetricsCollectorSnapshotIncludesPressureAssessment(t *testing.T) {
	t.Parallel()

	collector := newReadMetricsCollector(func() DBPoolStats {
		return DBPoolStats{MaxConns: 10, AcquiredConns: 9}
	})
	collector.SetThresholds(OverloadThresholds{
		DBPoolSaturationPct:    80,
		RequestQueueDepth:      2,
		UpstreamTimeoutRatePct: 5,
	})
	collector.begin("aggregate_records")
	collector.finish("aggregate_records", 504, 3*time.Millisecond, "count", "transactions|count|amount|group:and()", "transactions", 1, 0, "timeout")
	collector.begin("query_records")
	collector.begin("list_records")
	collector.finish("query_records", 429, time.Millisecond, "", "", "transactions", 0, 5000, "overloaded")

	snapshot := collector.snapshot()
	if snapshot.Pressure.Status != "critical" {
		t.Fatalf("pressure status = %q, want critical", snapshot.Pressure.Status)
	}
	if snapshot.Pressure.DBPoolSaturationPct != 90 {
		t.Fatalf("db pool saturation pct = %d, want 90", snapshot.Pressure.DBPoolSaturationPct)
	}
	if snapshot.Pressure.AggregateTimeoutRatePct != 100 {
		t.Fatalf("aggregate timeout rate pct = %d, want 100", snapshot.Pressure.AggregateTimeoutRatePct)
	}
}

func TestReadMetricsCollectorSnapshotTracksListLimits(t *testing.T) {
	t.Parallel()

	collector := newReadMetricsCollector(nil)
	collector.begin("list_records")
	collector.finish("list_records", 200, 2*time.Millisecond, "", "", "transactions", 0, 100, "")
	collector.begin("list_records")
	collector.finish("list_records", 200, 3*time.Millisecond, "", "", "transactions", 0, 5000, "")

	snapshot := collector.snapshot()
	endpoint := snapshot.Endpoints["list_records"]
	if endpoint.ListLimitCounts[100] != 1 {
		t.Fatalf("list limit count for 100 = %d, want 1", endpoint.ListLimitCounts[100])
	}
	if endpoint.ListLimitCounts[5000] != 1 {
		t.Fatalf("list limit count for 5000 = %d, want 1", endpoint.ListLimitCounts[5000])
	}
	if endpoint.LastListLimit != 5000 {
		t.Fatalf("last list limit = %d, want 5000", endpoint.LastListLimit)
	}
	if endpoint.MaxListLimit != 5000 {
		t.Fatalf("max list limit = %d, want 5000", endpoint.MaxListLimit)
	}
}
