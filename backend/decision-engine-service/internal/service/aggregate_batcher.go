package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

const aggregateBatchCollectionWindow = time.Millisecond

type aggregateBatchItem struct {
	query  ports.AggregateQuery
	result chan aggregateBatchResult
}

type aggregateBatchResult struct {
	value any
	err   error
}

// requestAggregateBatcher combines compatible aggregate requests that become
// ready while all live scenarios for one object are evaluated concurrently.
// It only combines queries constrained by the same user-selected projection
// value, so batching never widens an account query into a table-wide scan.
type requestAggregateBatcher struct {
	ctx               context.Context
	tenantID          string
	reader            ports.TenantDataReader
	model             ports.TenantModel
	semaphore         chan struct{}
	remoteConcurrency int

	mu      sync.Mutex
	pending []*aggregateBatchItem
	timer   *time.Timer
}

func newRequestAggregateBatcher(
	ctx context.Context,
	tenantID string,
	reader ports.TenantDataReader,
	model ports.TenantModel,
	remoteConcurrency int,
) *requestAggregateBatcher {
	limit := remoteConcurrency
	if limit <= 0 || limit > 4 {
		limit = 4
	}
	return &requestAggregateBatcher{
		ctx: ctx, tenantID: tenantID, reader: reader, model: model,
		semaphore: make(chan struct{}, limit), remoteConcurrency: remoteConcurrency,
	}
}

func (b *requestAggregateBatcher) Aggregate(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	if b == nil {
		return nil, fmt.Errorf("aggregate batcher is not configured")
	}
	if tenantID != b.tenantID {
		return b.aggregateOne(ctx, tenantID, query)
	}
	if _, ok := aggregateProjectionGroupKey(query, b.model); !ok {
		return b.aggregateOne(ctx, tenantID, query)
	}
	item := &aggregateBatchItem{query: query, result: make(chan aggregateBatchResult, 1)}
	b.mu.Lock()
	b.pending = append(b.pending, item)
	if b.timer == nil {
		b.timer = time.AfterFunc(aggregateBatchCollectionWindow, b.flush)
	}
	b.mu.Unlock()

	select {
	case result := <-item.result:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *requestAggregateBatcher) aggregateOne(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	release, err := asteval.AcquireAggregateRemoteSlot(ctx, b.remoteConcurrency)
	if err != nil {
		return nil, err
	}
	defer release()
	return b.reader.AggregateRecords(ctx, tenantID, query)
}

func (b *requestAggregateBatcher) flush() {
	b.mu.Lock()
	items := b.pending
	b.pending = nil
	b.timer = nil
	b.mu.Unlock()
	if len(items) == 0 {
		return
	}

	groups := make(map[string][]*aggregateBatchItem)
	for _, item := range items {
		key, ok := aggregateProjectionGroupKey(item.query, b.model)
		if !ok {
			key = fmt.Sprintf("single:%p", item)
		}
		groups[key] = append(groups[key], item)
	}
	for key, group := range groups {
		key, group := key, group
		go b.runGroup(key, group)
	}
}

func (b *requestAggregateBatcher) runGroup(groupKey string, items []*aggregateBatchItem) {
	select {
	case b.semaphore <- struct{}{}:
		defer func() { <-b.semaphore }()
	case <-b.ctx.Done():
		b.finishGroup(items, nil, b.ctx.Err())
		return
	}
	release, err := asteval.AcquireAggregateRemoteSlot(b.ctx, b.remoteConcurrency)
	if err != nil {
		b.finishGroup(items, nil, err)
		return
	}
	defer release()

	if len(items) == 1 {
		value, err := b.reader.AggregateRecords(b.ctx, b.tenantID, items[0].query)
		b.finishGroup(items, []any{value}, err)
		return
	}
	batchReader, ok := b.reader.(ports.BatchTenantDataReader)
	if !ok {
		values := make([]any, len(items))
		for i, item := range items {
			value, err := b.reader.AggregateRecords(b.ctx, b.tenantID, item.query)
			if err != nil {
				b.finishGroup(items, nil, err)
				return
			}
			values[i] = value
		}
		b.finishGroup(items, values, nil)
		return
	}

	for start := 0; start < len(items); start += 64 {
		end := start + 64
		if end > len(items) {
			end = len(items)
		}
		queries := make([]ports.AggregateQuery, end-start)
		for i := start; i < end; i++ {
			queries[i-start] = items[i].query
		}
		started := time.Now()
		values, err := batchReader.BatchAggregateRecords(b.ctx, b.tenantID, queries)
		slog.Default().Debug("event aggregate batch executed",
			"tenant_id", b.tenantID, "group", groupKey, "metric_count", len(queries),
			"duration_us", time.Since(started).Microseconds(), "error", err)
		if err != nil {
			b.finishGroup(items[start:end], nil, err)
			continue
		}
		b.finishGroup(items[start:end], values, nil)
	}
}

func (b *requestAggregateBatcher) finishGroup(items []*aggregateBatchItem, values []any, err error) {
	for i, item := range items {
		var value any
		if err == nil && i < len(values) {
			value = values[i]
		}
		item.result <- aggregateBatchResult{value: value, err: err}
	}
}

func aggregateProjectionGroupKey(query ports.AggregateQuery, model ports.TenantModel) (string, bool) {
	table, ok := model.Tables[query.ObjectType]
	if !ok || table.StorageClass != "event" || query.Filter == nil {
		return "", false
	}
	candidates := projectedEqualityPredicates(*query.Filter, table.Fields, true)
	if len(candidates) == 0 {
		return "", false
	}
	sort.Strings(candidates)
	return query.ObjectType + "|" + candidates[0], true
}

func projectedEqualityPredicates(
	filter ports.AggregateFilter,
	fields map[string]ports.TenantModelField,
	constrainsAllRows bool,
) []string {
	if !constrainsAllRows {
		return nil
	}
	kind := strings.ToLower(strings.TrimSpace(filter.Kind))
	if kind == "predicate" {
		field, ok := fields[filter.Field]
		if !ok {
			return nil
		}
		if !field.IsProjection || strings.ToLower(strings.TrimSpace(filter.Op)) != "eq" {
			return nil
		}
		value, err := json.Marshal(filter.Value)
		if err != nil {
			return nil
		}
		return []string{filter.Field + "=" + string(value)}
	}
	if kind != "" && kind != "group" {
		return nil
	}
	operator := strings.ToLower(strings.TrimSpace(filter.Operator))
	if operator == "" {
		operator = "and"
	}
	if operator != "and" {
		return nil
	}
	var candidates []string
	for _, child := range filter.Children {
		candidates = append(candidates, projectedEqualityPredicates(child, fields, true)...)
	}
	return candidates
}
