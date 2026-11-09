package mysqlapi

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"sync"
	"testing"
	"time"
)

func TestEvaluationTimingIsOptInDetachedAndConcurrent(t *testing.T) {
	base := context.Background()
	ports.RecordEvaluationDuration(base, "model", time.Second)
	if got := evaluationServerTiming(base); got != "" {
		t.Fatal("implicit timing disclosure", got)
	}
	ctx := ports.WithEvaluationTrace(base)
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() { defer group.Done(); ports.RecordEvaluationDuration(ctx, "model", time.Millisecond) }()
	}
	group.Wait()
	copy := ports.EvaluationTraceFromContext(ctx).Snapshot()
	copy["model"] = time.Hour
	ports.RecordEvaluationDuration(ctx, "bad\r\nname", time.Second)
	if got := evaluationServerTiming(ctx); got != "model;dur=20.000" {
		t.Fatal("racy, shared or unsafe diagnostic header", got)
	}
	if got := evaluationServerTiming(ports.WithEvaluationTrace(base)); got != "" {
		t.Fatal("request leaked timings", got)
	}
}
