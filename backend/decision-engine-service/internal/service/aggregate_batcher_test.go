package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	sharedeventstore "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service"
)

type aggregateBatchReaderStub struct {
	mu              sync.Mutex
	individualCalls int
	batchCalls      int
}

func (s *aggregateBatchReaderStub) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}

func (s *aggregateBatchReaderStub) ListRecords(context.Context, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}

func (s *aggregateBatchReaderStub) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}

func (s *aggregateBatchReaderStub) AggregateRecords(context.Context, string, ports.AggregateQuery) (any, error) {
	s.mu.Lock()
	s.individualCalls++
	s.mu.Unlock()
	return float64(1), nil
}

func (s *aggregateBatchReaderStub) BatchAggregateRecords(_ context.Context, _ string, queries []ports.AggregateQuery) ([]any, error) {
	s.mu.Lock()
	s.batchCalls++
	s.mu.Unlock()
	values := make([]any, len(queries))
	for i := range values {
		values[i] = float64(i + 1)
	}
	return values, nil
}

func TestRequestAggregateBatcherCombinesSameProjectedEntity(t *testing.T) {
	reader := &aggregateBatchReaderStub{}
	model := projectedAccountModel()
	batcher := newRequestAggregateBatcher(context.Background(), "tenant-1", reader, model, 4)
	queries := []ports.AggregateQuery{
		{ObjectType: "transactions", Aggregate: "count", Field: "amount", Filter: accountFilter("acct-1", "2026-08-01T00:00:00Z")},
		{ObjectType: "transactions", Aggregate: "sum", Field: "amount", Filter: accountFilter("acct-1", "2026-07-01T00:00:00Z")},
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(queries))
	for _, query := range queries {
		query := query
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := batcher.Aggregate(context.Background(), "tenant-1", query)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.batchCalls != 1 || reader.individualCalls != 0 {
		t.Fatalf("batch calls = %d, individual calls = %d; want 1 and 0", reader.batchCalls, reader.individualCalls)
	}
}

func TestRequestAggregateBatcherDoesNotCombineDifferentProjectedEntities(t *testing.T) {
	reader := &aggregateBatchReaderStub{}
	batcher := newRequestAggregateBatcher(context.Background(), "tenant-1", reader, projectedAccountModel(), 4)
	queries := []ports.AggregateQuery{
		{ObjectType: "transactions", Aggregate: "count", Field: "amount", Filter: accountFilter("acct-1", "2026-08-01T00:00:00Z")},
		{ObjectType: "transactions", Aggregate: "count", Field: "amount", Filter: accountFilter("acct-2", "2026-08-01T00:00:00Z")},
	}

	var wg sync.WaitGroup
	for _, query := range queries {
		query := query
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := batcher.Aggregate(context.Background(), "tenant-1", query); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.batchCalls != 0 || reader.individualCalls != 2 {
		t.Fatalf("batch calls = %d, individual calls = %d; want 0 and 2", reader.batchCalls, reader.individualCalls)
	}
}

func projectedAccountModel() ports.TenantModel {
	return ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"transactions": {
			Name: "transactions", StorageClass: "event",
			Fields: map[string]ports.TenantModelField{
				"account_ref": {Name: "account_ref", Type: "string", IsProjection: true},
				"date":        {Name: "date", Type: "timestamp"},
				"amount":      {Name: "amount", Type: "float"},
			},
		},
	}}
}

func TestAggregateBatcherBatchesColdPolicyManagedFields(t *testing.T) {
	model := projectedAccountModel()
	table := model.Tables["transactions"]
	field := table.Fields["account_ref"]
	field.AggregationMode = sharedeventstore.AggregationModeAdaptiveCache
	table.Fields["account_ref"] = field
	model.Tables["transactions"] = table
	if _, ok := aggregateProjectionGroupKey(ports.AggregateQuery{
		ObjectType: "transactions", Aggregate: "count", Field: "amount",
		Filter: accountFilter("acct-1", "2026-07-01T00:00:00Z"),
	}, model); !ok {
		t.Fatal("cold policy-managed aggregate was not admitted to the projection batch")
	}
}

func accountFilter(account, lowerBound string) *ports.AggregateFilter {
	return &ports.AggregateFilter{Kind: "group", Operator: "and", Children: []ports.AggregateFilter{
		{Kind: "predicate", Field: "account_ref", Op: "eq", Value: account},
		{Kind: "predicate", Field: "date", Op: "gte", Value: lowerBound},
	}}
}

func TestEvaluationTimeForRequestUsesEventField(t *testing.T) {
	fallback := time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)
	model := ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"transactions": {StorageClass: "event", EventTimeField: "date"},
	}}
	got := evaluationTimeForRequest(DecisionEvaluationRequest{
		ObjectType: "transactions", Fields: map[string]any{"date": "2026-07-01T02:03:04Z"},
	}, model, fallback)
	if want := time.Date(2026, time.July, 1, 2, 3, 4, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("evaluation time = %s, want %s", got, want)
	}
}
