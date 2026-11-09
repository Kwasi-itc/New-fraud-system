package ast_eval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type batchedAggregateFixture struct {
	evaluatorTenantDataReaderStub
	calls, active, peak atomic.Int32
}

func (r *batchedAggregateFixture) AggregateRecordsBatch(ctx context.Context, tenant string, queries []ports.AggregateQuery) ([]any, error) {
	r.calls.Add(1)
	active := r.active.Add(1)
	defer r.active.Add(-1)
	for {
		old := r.peak.Load()
		if old >= active || r.peak.CompareAndSwap(old, active) {
			break
		}
	}
	select {
	case <-time.After(5 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	values := make([]any, len(queries))
	for i, q := range queries {
		values[i] = q.Aggregate
	}
	return values, nil
}

func TestAggregateBatcherDemandGroupingBudgetsAndCancellation(t *testing.T) {
	executor, err := NewAggregateExecutor(2, 100*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	reader := &batchedAggregateFixture{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batcher := NewAggregateBatcher(ctx, "tenant", reader, executor)
	var group sync.WaitGroup
	errorsCh := make(chan error, 24)
	for i := 0; i < 24; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			q := ports.AggregateQuery{ObjectType: "transactions", Aggregate: "count", Field: "amount", Filter: &ports.AggregateFilter{Kind: "predicate", Field: "account", Op: "eq", Value: i % 3}}
			value, err := batcher.Evaluate(ctx, q)
			if err == nil && value != "count" {
				err = errors.New("wrong result")
			}
			errorsCh <- err
		}(i)
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	queries, demands := executor.Stats()
	if reader.peak.Load() > 2 || queries >= 24 || demands != 24 || queries != uint64(reader.calls.Load()) {
		t.Fatal("grouping/admission", queries, demands, reader.peak.Load())
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err := batcher.Evaluate(cancelled, ports.AggregateQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// A waiting caller must observe the same deadline as SQL execution, even
	// while every permit is held. Separate runtimes retain independent budgets.
	blocked, _ := NewAggregateExecutor(1, 5*time.Millisecond, 0)
	blocked.slots <- struct{}{}
	if _, err := blocked.run(ctx, 1, func(context.Context) ([]any, error) { t.Error("entered saturated budget"); return nil, nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-blocked.slots
	if _, err := executor.run(ctx, 1, func(context.Context) ([]any, error) { return []any{1}, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestFormulaPlansIsolationBoundedEvictionAndDetachedEvidence(t *testing.T) {
	plans, err := NewFormulaPlans(2, 4096)
	if err != nil {
		t.Fatal(err)
	}
	runtime := Runtime{TenantID: "tenant-a", PlanScope: "iteration:model", FormulaPlans: plans, Fields: map[string]any{"amount": 10}}
	formula := json.RawMessage(`{"function":"gt","children":[{"function":"payload","children":[{"constant":"amount"}]},{"constant":5}]}`)
	for _, amount := range []int{10, 0, 20} {
		runtime.Fields["amount"] = amount
		got, _, err := EvaluateFormulaWithEvidence(context.Background(), formula, runtime)
		if err != nil || got != (amount > 5) {
			t.Fatal(got, err)
		}
	}
	if plans.lru.Len() != 1 {
		t.Fatal("program not reused", plans.lru.Len())
	}
	for _, scope := range []string{"other-iteration:model", "iteration:changed-model"} {
		runtime.PlanScope = scope
		if _, err := EvaluateFormula(context.Background(), formula, runtime); err != nil {
			t.Fatal(err)
		}
	}
	if plans.lru.Len() != 2 || plans.bytes > plans.maxBytes {
		t.Fatal("unbounded", plans.lru.Len(), plans.bytes)
	}
	runtime.TenantID = "tenant-b"
	if _, err := EvaluateFormula(context.Background(), formula, runtime); err != nil {
		t.Fatal(err)
	}
	mutable := json.RawMessage(`{"constant":{"nested":["original"]}}`)
	node, err := plans.parse(mutable, runtime)
	if err != nil {
		t.Fatal(err)
	}
	first, err := evaluateNodeWithEvidence(context.Background(), node, runtime)
	if err != nil {
		t.Fatal(err)
	}
	first.ReturnValue.(map[string]any)["nested"].([]any)[0] = "modified"
	second, err := evaluateNodeWithEvidence(context.Background(), node, runtime)
	if err != nil || second.ReturnValue.(map[string]any)["nested"].([]any)[0] != "original" {
		t.Fatal("program mutated", second, err)
	}
}

func TestValidatedFormulaScopeNeverCachesFailuresOrCrossesModels(t *testing.T) {
	plans, err := NewFormulaPlans(4, 4096)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	failure := errors.New("model validation failed")
	validate := func() error { calls++; return failure }
	if err := plans.ValidateScope("tenant-a", "iteration:model-a", validate); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	valid := func() error { calls++; return nil }
	if err := plans.ValidateScope("tenant-a", "iteration:model-a", valid); err != nil {
		t.Fatal(err)
	}
	if err := plans.ValidateScope("tenant-a", "iteration:model-a", validate); err != nil || calls != 2 {
		t.Fatal("valid plan not reused", calls, err)
	}
	if err := plans.ValidateScope("tenant-a", "iteration:model-b", validate); !errors.Is(err, failure) {
		t.Fatal("changed model reused validation", err)
	}
	if err := plans.ValidateScope("tenant-b", "iteration:model-a", validate); !errors.Is(err, failure) {
		t.Fatal("foreign tenant reused validation", err)
	}
	if calls != 4 {
		t.Fatal(calls)
	}
}
