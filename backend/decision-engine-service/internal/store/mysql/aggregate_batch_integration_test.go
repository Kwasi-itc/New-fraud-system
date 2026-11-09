package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

func TestMySQLAggregateBatchOneAuthorizationExactValuesAndIsolation(t *testing.T) {
	f := tenantRegistryFixture(t)
	f.insert(t, "one", "A", "recipient", 10)
	f.insert(t, "two", "A", nil, 20)
	f.insert(t, "three", "B", "recipient", 30)
	reader := f.reader.WithModel(f.tenant.String(), f.model).(TenantDataReader)
	configuration := *reader.configuration
	var reads atomic.Int32
	configuration.authorize = func(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, write bool) error {
		reads.Add(1)
		return allowPlatformFixture(ctx, tx, tenant, write)
	}
	reader.configuration = &configuration
	filter := &ports.AggregateFilter{Kind: "predicate", Field: "wallet_number_hash", Op: "eq", Value: "A"}
	queries := []ports.AggregateQuery{}
	for _, q := range []struct{ aggregate, field string }{{"count", "object_id"}, {"sum", "amount"}, {"avg", "amount"}, {"min", "amount"}, {"max", "amount"}, {"count_distinct", "user_id"}, {"count", "user_id"}} {
		queries = append(queries, ports.AggregateQuery{ObjectType: "wallets", Aggregate: q.aggregate, Field: q.field, Filter: filter})
	}
	values, err := reader.AggregateRecordsBatch(context.Background(), f.tenant.String(), queries)
	if err != nil || fmt.Sprint(values) != "[2 30 15 10 20 1 1]" || reads.Load() != 1 {
		t.Fatal(values, err, reads.Load())
	}
	for i, q := range queries {
		value, err := reader.AggregateRecords(context.Background(), f.tenant.String(), q)
		if err != nil || fmt.Sprint(value) != fmt.Sprint(values[i]) {
			t.Fatal(q, value, err)
		}
	}
	for _, bad := range []ports.AggregateQuery{
		{ObjectType: "another_table", Aggregate: "count", Field: "object_id", Filter: filter},
		{ObjectType: "wallets", Aggregate: "count", Field: "object_id", Filter: &ports.AggregateFilter{Kind: "predicate", Field: "wallet_number_hash", Op: "eq", Value: "B"}},
		{ObjectType: "wallets", Aggregate: "invalid", Field: "amount", Filter: filter},
	} {
		if result, err := reader.AggregateRecordsBatch(context.Background(), f.tenant.String(), []ports.AggregateQuery{queries[0], bad}); err == nil || result != nil {
			t.Fatal("mixed/invalid batch", result, err)
		}
	}
	if result, err := reader.AggregateRecordsBatch(context.Background(), uuid.NewString(), queries); !errors.Is(err, ErrTenantScope) || result != nil {
		t.Fatal("tenant scope", result, err)
	}
	filter.Value = "absent"
	values, err = reader.AggregateRecordsBatch(context.Background(), f.tenant.String(), queries)
	if err != nil || fmt.Sprint(values) != "[0 0 <nil> <nil> <nil> 0 0]" {
		t.Fatal("empty/null semantics", values, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := reader.AggregateRecordsBatch(ctx, f.tenant.String(), queries); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal("cancel", result, err)
	}
	if result, err := reader.AggregateRecordsBatch(context.Background(), f.tenant.String(), make([]ports.AggregateQuery, 17)); !errors.Is(err, ErrProjectionLimit) || result != nil {
		t.Fatal("bound", result, err)
	}
}
