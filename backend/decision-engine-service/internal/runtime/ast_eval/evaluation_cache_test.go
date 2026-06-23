package ast_eval

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type countingCustomListRepo struct {
	mu       sync.Mutex
	contains int
}

func (r *countingCustomListRepo) Create(ctx context.Context, item platform.CustomListEntry) (platform.CustomListEntry, error) {
	return item, nil
}

func (r *countingCustomListRepo) ListByName(ctx context.Context, tenantID, listName string) ([]platform.CustomListEntry, error) {
	return nil, nil
}

func (r *countingCustomListRepo) Contains(ctx context.Context, tenantID, listName, value string) (bool, error) {
	r.mu.Lock()
	r.contains++
	r.mu.Unlock()
	return true, nil
}

func (r *countingCustomListRepo) ContainsCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.contains
}

type countingTenantDataReader struct {
	mu         sync.Mutex
	queries    int
	aggregates int
	aggregate  any
	err        error
	delay      time.Duration
	records    []ports.TenantRecord
}

func (r *countingTenantDataReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}

func (r *countingTenantDataReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	return r.records, nil
}

func (r *countingTenantDataReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	r.mu.Lock()
	r.queries++
	r.mu.Unlock()
	return r.records, nil
}

func (r *countingTenantDataReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	r.mu.Lock()
	r.aggregates++
	r.mu.Unlock()
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.aggregate, nil
}

func (r *countingTenantDataReader) QueryCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queries
}

func (r *countingTenantDataReader) AggregateCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.aggregates
}

func TestEvaluationCacheReusesCustomListContains(t *testing.T) {
	t.Parallel()

	repo := &countingCustomListRepo{}
	node := domainast.Node{
		Function: "in_custom_list",
		NamedChildren: map[string]domainast.Node{
			"list":  {Constant: "blocked_emails"},
			"value": {Constant: "user@example.com"},
		},
	}
	runtime := Runtime{
		TenantID:       "tenant-1",
		ObjectID:       "txn-1",
		ObjectType:     "transactions",
		Fields:         map[string]any{},
		Now:            time.Unix(100, 0),
		CustomListRepo: repo,
		EvalCache:      NewEvaluationCache(),
	}

	for i := 0; i < 5; i++ {
		value, err := EvaluateNode(context.Background(), node, runtime)
		if err != nil {
			t.Fatalf("EvaluateNode() error = %v", err)
		}
		if value != true {
			t.Fatalf("EvaluateNode() = %#v, want true", value)
		}
	}
	if got := repo.ContainsCalls(); got != 1 {
		t.Fatalf("Contains calls = %d, want 1", got)
	}
}

func TestEvaluationCacheReusesRelatedFieldTraversal(t *testing.T) {
	t.Parallel()

	reader := &countingTenantDataReader{
		records: []ports.TenantRecord{{
			ObjectID:   "acct-1",
			ObjectType: "accounts",
			Fields:     map[string]any{"account_status": "active"},
		}},
	}
	node := domainast.Node{
		Function: "related_field",
		NamedChildren: map[string]domainast.Node{
			"path":  {Constant: "account"},
			"field": {Constant: "account_status"},
		},
	}
	runtime := Runtime{
		TenantID:         "tenant-1",
		ObjectID:         "txn-1",
		ObjectType:       "transactions",
		Fields:           map[string]any{"account_id": "acct-1"},
		Now:              time.Unix(100, 0),
		TenantDataReader: reader,
		Model: &ports.TenantModel{Tables: map[string]ports.TenantModelTable{
			"transactions": {
				Name: "transactions",
				LinksToSingle: map[string]ports.TenantModelLink{
					"account": {
						ParentTableName: "accounts",
						ParentFieldName: "object_id",
						ChildFieldName:  "account_id",
					},
				},
			},
			"accounts": {
				Name: "accounts",
				Fields: map[string]ports.TenantModelField{
					"account_status": {Name: "account_status", Type: "string"},
				},
			},
		}},
		EvalCache: NewEvaluationCache(),
	}

	for i := 0; i < 5; i++ {
		value, err := EvaluateNode(context.Background(), node, runtime)
		if err != nil {
			t.Fatalf("EvaluateNode() error = %v", err)
		}
		if value != "active" {
			t.Fatalf("EvaluateNode() = %#v, want active", value)
		}
	}
	if got := reader.QueryCalls(); got != 1 {
		t.Fatalf("QueryRecords calls = %d, want 1", got)
	}
}

func TestEvaluationCacheSingleflightsConcurrentAggregate(t *testing.T) {
	t.Parallel()

	reader := &countingTenantDataReader{aggregate: int64(7), delay: 25 * time.Millisecond}
	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "object_id"},
			"aggregator": {Constant: "COUNT"},
		},
	}
	runtime := Runtime{
		TenantID:         "tenant-1",
		ObjectID:         "txn-1",
		ObjectType:       "transactions",
		Fields:           map[string]any{},
		Now:              time.Unix(100, 0),
		TenantDataReader: reader,
		EvalCache:        NewEvaluationCache(),
	}

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := EvaluateNode(context.Background(), node, runtime)
			if err != nil {
				errs <- err
				return
			}
			if value != int64(7) {
				errs <- fmt.Errorf("EvaluateNode() = %#v, want 7", value)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := reader.AggregateCalls(); got != 1 {
		t.Fatalf("AggregateRecords calls = %d, want 1", got)
	}
}

func TestEvaluationCacheDoesNotCacheErrors(t *testing.T) {
	t.Parallel()

	reader := &countingTenantDataReader{err: fmt.Errorf("boom")}
	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "object_id"},
			"aggregator": {Constant: "COUNT"},
		},
	}
	runtime := Runtime{
		TenantID:              "tenant-1",
		ObjectID:              "txn-1",
		ObjectType:            "transactions",
		Fields:                map[string]any{},
		Now:                   time.Unix(100, 0),
		TenantDataReader:      reader,
		AggregatePushdownMode: AggregatePushdownModeStrict,
		EvalCache:             NewEvaluationCache(),
	}

	for i := 0; i < 2; i++ {
		if _, err := EvaluateNode(context.Background(), node, runtime); err == nil {
			t.Fatal("EvaluateNode() error = nil, want error")
		}
	}
	if got := reader.AggregateCalls(); got != 2 {
		t.Fatalf("AggregateRecords calls = %d, want 2", got)
	}
}
