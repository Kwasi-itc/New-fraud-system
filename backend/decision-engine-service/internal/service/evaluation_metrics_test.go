package service

import (
	"fmt"
	"testing"
	"time"
)

func TestEvaluationMetricsCollectorTracksFailuresAndPercentiles(t *testing.T) {
	t.Parallel()

	collector := newEvaluationMetricsCollector()
	collector.recordSingle(2*time.Millisecond, true, nil)
	collector.recordSingle(8*time.Millisecond, false, fmt.Errorf("aggregate pushdown failed: unexpected status from ingestion-service: 503"))
	collector.recordSingle(5*time.Millisecond, false, fmt.Errorf("object_type does not match scenario trigger object type"))

	snapshot := collector.snapshot()
	if snapshot.SingleScenario.Requests != 3 {
		t.Fatalf("requests = %d, want 3", snapshot.SingleScenario.Requests)
	}
	if snapshot.SingleScenario.Successes != 1 {
		t.Fatalf("successes = %d, want 1", snapshot.SingleScenario.Successes)
	}
	if snapshot.SingleScenario.Failures != 2 {
		t.Fatalf("failures = %d, want 2", snapshot.SingleScenario.Failures)
	}
	if snapshot.SingleScenario.Triggered != 1 {
		t.Fatalf("triggered = %d, want 1", snapshot.SingleScenario.Triggered)
	}
	if snapshot.SingleScenario.FailureCategories["dependency_failure"] != 1 {
		t.Fatalf("dependency failures = %d, want 1", snapshot.SingleScenario.FailureCategories["dependency_failure"])
	}
	if snapshot.SingleScenario.FailureCategories["evaluation_invalid"] != 1 {
		t.Fatalf("evaluation_invalid failures = %d, want 1", snapshot.SingleScenario.FailureCategories["evaluation_invalid"])
	}
	if snapshot.SingleScenario.P50LatencyMicros == 0 || snapshot.SingleScenario.P95LatencyMicros == 0 || snapshot.SingleScenario.P99LatencyMicros == 0 {
		t.Fatalf("expected non-zero latency percentiles, got %+v", snapshot.SingleScenario)
	}
}
