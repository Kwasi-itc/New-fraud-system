package ports

import (
	"context"
	"sync"
	"time"
)

// EvaluationTrace is optional request-local diagnostic state. It carries no
// identity, payload or authority. Outer stages and their substages overlap.
type EvaluationTrace struct {
	mu     sync.Mutex
	stages map[string]time.Duration
}
type evaluationTraceKey struct{}

func WithEvaluationTrace(ctx context.Context) context.Context {
	return context.WithValue(ctx, evaluationTraceKey{}, &EvaluationTrace{stages: make(map[string]time.Duration)})
}
func EvaluationTraceFromContext(ctx context.Context) *EvaluationTrace {
	t, _ := ctx.Value(evaluationTraceKey{}).(*EvaluationTrace)
	return t
}
func RecordEvaluationDuration(ctx context.Context, stage string, elapsed time.Duration) {
	if t := EvaluationTraceFromContext(ctx); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		// Only internal stage names are supplied. Bound accidental cardinality.
		if len(t.stages) < 64 || t.stages[stage] != 0 {
			t.stages[stage] += elapsed
		}
	}
}
func (t *EvaluationTrace) Snapshot() map[string]time.Duration {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]time.Duration, len(t.stages))
	for k, v := range t.stages {
		out[k] = v
	}
	return out
}
