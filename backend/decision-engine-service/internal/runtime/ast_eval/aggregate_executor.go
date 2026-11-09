package ast_eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// AggregateExecutor is owned by a runtime, shared across its tenants and worker
// scopes. Admission and SQL execution share one deadline and fixed semaphore;
// changing another runtime's policy cannot replace its slots.
type AggregateExecutor struct {
	slots            chan struct{}
	timeout, window  time.Duration
	queries, demands atomic.Uint64
}

func NewAggregateExecutor(limit int, timeout, window time.Duration) (*AggregateExecutor, error) {
	if limit < 1 || limit > 64 || timeout <= 0 || timeout > 15*time.Second || window < 0 || window > time.Millisecond {
		return nil, errors.New("bounded aggregate concurrency, deadline and collection window required")
	}
	return &AggregateExecutor{slots: make(chan struct{}, limit), timeout: timeout, window: window}, nil
}

func (e *AggregateExecutor) Stats() (queries, demands uint64) {
	return e.queries.Load(), e.demands.Load()
}

func (e *AggregateExecutor) run(ctx context.Context, count int, work func(context.Context) ([]any, error)) ([]any, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	started := time.Now()
	select {
	case e.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
	defer func() { <-e.slots }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recordAggregateRemoteLimiterWait(time.Since(started))
	e.queries.Add(1)
	e.demands.Add(uint64(count))
	return work(ctx)
}

type aggregateReply struct {
	value any
	err   error
}
type aggregateDemand struct {
	ctx   context.Context
	query ports.AggregateQuery
	reply chan aggregateReply
}
type aggregateGroup struct{ demands []aggregateDemand }

// One batcher per evaluation. Only demands that actually reached an aggregate
// are collected; triggers, short-circuited branches and snoozed rules are never
// prefetched. Filter keys include every resolved payload/time value.
type AggregateBatcher struct {
	ctx      context.Context
	tenant   string
	reader   ports.TenantDataReader
	executor *AggregateExecutor
	mu       sync.Mutex
	pending  map[string]*aggregateGroup
	queued   int
}

func NewAggregateBatcher(ctx context.Context, tenant string, reader ports.TenantDataReader, executor *AggregateExecutor) *AggregateBatcher {
	if executor == nil {
		return nil
	}
	return &AggregateBatcher{ctx: ctx, tenant: tenant, reader: reader, executor: executor, pending: make(map[string]*aggregateGroup)}
}

func (b *AggregateBatcher) Evaluate(ctx context.Context, query ports.AggregateQuery) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	batchReader, capable := b.reader.(ports.AggregateBatchReader)
	if !capable || b.executor.window == 0 {
		values, err := b.executor.run(ctx, 1, func(c context.Context) ([]any, error) {
			v, e := b.reader.AggregateRecords(c, b.tenant, query)
			return []any{v}, e
		})
		if err != nil {
			return nil, err
		}
		return values[0], nil
	}
	keyBytes, err := json.Marshal(struct {
		Table  string
		Filter *ports.AggregateFilter
	}{query.ObjectType, query.Filter})
	if err != nil || len(keyBytes) > 1<<20 {
		return nil, errors.New("aggregate group key exceeds bound")
	}
	digest := sha256.Sum256(keyBytes)
	key := string(digest[:])
	demand := aggregateDemand{ctx, query, make(chan aggregateReply, 1)}
	b.mu.Lock()
	if b.queued >= 128 {
		b.mu.Unlock()
		return nil, errors.New("aggregate demand limit exceeded")
	}
	b.queued++
	defer func() { b.mu.Lock(); b.queued--; b.mu.Unlock() }()
	group := b.pending[key]
	if group == nil {
		if len(b.pending) >= 64 {
			b.mu.Unlock()
			return nil, errors.New("aggregate pending group limit exceeded")
		}
		group = &aggregateGroup{}
		b.pending[key] = group
		go b.flush(key, group, batchReader)
	}
	group.demands = append(group.demands, demand)
	// Each group is bounded independently; the next demand starts a new group.
	if len(group.demands) == 16 {
		delete(b.pending, key)
	}
	b.mu.Unlock()
	select {
	case result := <-demand.reply:
		return result.value, result.err
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func (b *AggregateBatcher) flush(key string, group *aggregateGroup, reader ports.AggregateBatchReader) {
	timer := time.NewTimer(b.executor.window)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-b.ctx.Done():
	}
	b.mu.Lock()
	if b.pending[key] == group {
		delete(b.pending, key)
	}
	demands := append([]aggregateDemand(nil), group.demands...)
	b.mu.Unlock()
	queries, active := make([]ports.AggregateQuery, 0, len(demands)), make([]aggregateDemand, 0, len(demands))
	for _, demand := range demands {
		if demand.ctx.Err() == nil {
			queries = append(queries, demand.query)
			active = append(active, demand)
		}
	}
	if len(active) == 0 {
		return
	}
	groupCtx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	var remaining atomic.Int32
	remaining.Store(int32(len(active)))
	stops := make([]func() bool, 0, len(active))
	for _, demand := range active {
		stops = append(stops, context.AfterFunc(demand.ctx, func() {
			if remaining.Add(-1) == 0 {
				cancel()
			}
		}))
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	values, err := b.executor.run(groupCtx, len(queries), func(ctx context.Context) ([]any, error) { return reader.AggregateRecordsBatch(ctx, b.tenant, queries) })
	if err == nil && len(values) != len(active) {
		err = errors.New("aggregate batch result cardinality mismatch")
	}
	for i, demand := range active {
		result := aggregateReply{err: err}
		if err == nil {
			result.value = values[i]
		}
		demand.reply <- result
	}
}
