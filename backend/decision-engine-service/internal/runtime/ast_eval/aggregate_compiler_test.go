package ast_eval

import (
	"context"
	"errors"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type aggregateTestTenantDataReader struct {
	aggregateValue any
	aggregateErr   error
	records        []ports.TenantRecord
}

func (s aggregateTestTenantDataReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	for _, record := range s.records {
		if record.ObjectType == objectType && record.ObjectID == objectID {
			return record, nil
		}
	}
	return ports.TenantRecord{}, nil
}

func (s aggregateTestTenantDataReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	out := make([]ports.TenantRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.ObjectType == objectType {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s aggregateTestTenantDataReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	return nil, nil
}

func (s aggregateTestTenantDataReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	if s.aggregateErr != nil {
		return nil, s.aggregateErr
	}
	return s.aggregateValue, nil
}

func TestCompileAggregateQueryResolvesPayloadAndTimeValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "SUM"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "owner_id"},
							"operator":  {Constant: "="},
							"value":     {Function: "Payload", Children: []domainast.Node{{Constant: "owner_id"}}},
						},
					},
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "created_at"},
							"operator":  {Constant: ">="},
							"value": {
								Function: "TimeAdd",
								NamedChildren: map[string]domainast.Node{
									"timestampField": {Function: "Payload", Children: []domainast.Node{{Constant: "created_at"}}},
									"duration":       {Constant: "PT24H"},
									"sign":           {Constant: "-"},
								},
							},
						},
					},
				},
			},
		},
	}

	result, err := CompileAggregateQuery(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Now:        now,
		Fields: map[string]any{
			"owner_id":   "customer-1",
			"created_at": now.Format(time.RFC3339),
		},
	})
	if err != nil {
		t.Fatalf("CompileAggregateQuery() error = %v", err)
	}
	if !result.Supported {
		t.Fatalf("CompileAggregateQuery() Supported = false, reason = %q", result.UnsupportedReason)
	}
	if result.Query.ObjectType != "transactions" {
		t.Fatalf("query object type = %q, want transactions", result.Query.ObjectType)
	}
	if result.Query.Aggregate != "sum" {
		t.Fatalf("query aggregate = %q, want sum", result.Query.Aggregate)
	}
	if result.Query.Field != "amount" {
		t.Fatalf("query field = %q, want amount", result.Query.Field)
	}
	if result.Query.Filter == nil || len(result.Query.Filter.Children) != 2 {
		t.Fatalf("query filter = %#v, want two AND children", result.Query.Filter)
	}
	if got := result.Query.Filter.Children[0].Value; got != "customer-1" {
		t.Fatalf("first filter value = %#v, want customer-1", got)
	}
	if got, ok := result.Query.Filter.Children[1].Value.(time.Time); !ok {
		t.Fatalf("second filter value type = %T, want time.Time", result.Query.Filter.Children[1].Value)
	} else if !got.Equal(now.Add(-24 * time.Hour)) {
		t.Fatalf("second filter value = %v, want %v", got, now.Add(-24*time.Hour))
	}
}

func TestCompileAggregateQueryRejectsUnsupportedOperator(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "owner_id"},
							"operator":  {Constant: "IsEmpty"},
						},
					},
				},
			},
		},
	}

	result, err := CompileAggregateQuery(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
	})
	if err != nil {
		t.Fatalf("CompileAggregateQuery() error = %v", err)
	}
	if result.Supported {
		t.Fatalf("CompileAggregateQuery() Supported = true, want false")
	}
	if result.UnsupportedReason == "" {
		t.Fatalf("CompileAggregateQuery() UnsupportedReason = empty")
	}
}

func TestCompileAggregateQueryRejectsUnsupportedAggregate(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "STDDEV"},
		},
	}

	result, err := CompileAggregateQuery(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
	})
	if err != nil {
		t.Fatalf("CompileAggregateQuery() error = %v", err)
	}
	if result.Supported {
		t.Fatalf("CompileAggregateQuery() Supported = true, want false")
	}
	if result.UnsupportedReason == "" {
		t.Fatalf("CompileAggregateQuery() UnsupportedReason = empty")
	}
}

func TestCompileAggregateQueryBuildsInFilter(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "List",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "status"},
							"operator":  {Constant: "IsInList"},
							"value": {
								Function: "List",
								Children: []domainast.Node{
									{Constant: "review"},
									{Constant: "decline"},
								},
							},
						},
					},
				},
			},
		},
	}

	result, err := CompileAggregateQuery(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
		Model: &ports.TenantModel{
			RecordLookupField: "object_id",
			Tables: map[string]ports.TenantModelTable{
				"transactions": {
					Name: "transactions",
					Fields: map[string]ports.TenantModelField{
						"status": {Name: "status", Type: "string"},
						"amount": {Name: "amount", Type: "number"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CompileAggregateQuery() error = %v", err)
	}
	if !result.Supported {
		t.Fatalf("CompileAggregateQuery() Supported = false, reason = %q", result.UnsupportedReason)
	}
	if got := result.Query.Filter.Children[0].Op; got != "in" {
		t.Fatalf("filter op = %q, want in", got)
	}
	items, ok := result.Query.Filter.Children[0].Value.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("filter value = %#v, want 2-item list", result.Query.Filter.Children[0].Value)
	}
}

func TestCompileAggregateQueryPreservesGroupedBooleanFilters(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "and",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "owner_id"},
							"operator":  {Constant: "="},
							"value":     {Constant: "customer-1"},
						},
					},
					{
						Function: "or",
						Children: []domainast.Node{
							{
								Function: "Filter",
								NamedChildren: map[string]domainast.Node{
									"tableName": {Constant: "transactions"},
									"fieldName": {Constant: "status"},
									"operator":  {Constant: "="},
									"value":     {Constant: "review"},
								},
							},
							{
								Function: "Filter",
								NamedChildren: map[string]domainast.Node{
									"tableName": {Constant: "transactions"},
									"fieldName": {Constant: "status"},
									"operator":  {Constant: "="},
									"value":     {Constant: "decline"},
								},
							},
						},
					},
				},
			},
		},
	}

	result, err := CompileAggregateQuery(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
	})
	if err != nil {
		t.Fatalf("CompileAggregateQuery() error = %v", err)
	}
	if !result.Supported {
		t.Fatalf("CompileAggregateQuery() Supported = false, reason = %q", result.UnsupportedReason)
	}
	if result.Query.Filter == nil {
		t.Fatalf("query filter = nil")
	}
	if result.Query.Filter.Operator != "and" || len(result.Query.Filter.Children) != 2 {
		t.Fatalf("top-level filter = %#v, want AND with 2 children", result.Query.Filter)
	}
	group := result.Query.Filter.Children[1]
	if group.Op != "in" {
		t.Fatalf("nested filter op = %q, want in after OR normalization", group.Op)
	}
	items, ok := group.Value.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("nested filter value = %#v, want 2-item IN list", group.Value)
	}
}

func TestCompileAggregateQueryPreservesNotFilter(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "not",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "status"},
							"operator":  {Constant: "="},
							"value":     {Constant: "approved"},
						},
					},
				},
			},
		},
	}

	result, err := CompileAggregateQuery(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
	})
	if err != nil {
		t.Fatalf("CompileAggregateQuery() error = %v", err)
	}
	if !result.Supported {
		t.Fatalf("CompileAggregateQuery() Supported = false, reason = %q", result.UnsupportedReason)
	}
	if result.Query.Filter == nil || result.Query.Filter.Operator != "not" || len(result.Query.Filter.Children) != 1 {
		t.Fatalf("query filter = %#v, want NOT with 1 child", result.Query.Filter)
	}
}

func TestCompileAggregateQueryDoesNotNormalizeMixedOrConditions(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "or",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "status"},
							"operator":  {Constant: "="},
							"value":     {Constant: "review"},
						},
					},
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "country"},
							"operator":  {Constant: "="},
							"value":     {Constant: "GH"},
						},
					},
				},
			},
		},
	}

	result, err := CompileAggregateQuery(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
	})
	if err != nil {
		t.Fatalf("CompileAggregateQuery() error = %v", err)
	}
	if !result.Supported {
		t.Fatalf("CompileAggregateQuery() Supported = false, reason = %q", result.UnsupportedReason)
	}
	if result.Query.Filter == nil || result.Query.Filter.Operator != "or" || len(result.Query.Filter.Children) != 2 {
		t.Fatalf("query filter = %#v, want OR group preserved", result.Query.Filter)
	}
}

func TestEvaluateMarbleAggregatorUsesRemoteAggregateResult(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:         "tenant-1",
		ObjectID:         "txn-1",
		ObjectType:       "transactions",
		Fields:           map[string]any{},
		TenantDataReader: aggregateTestTenantDataReader{aggregateValue: int64(7)},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != int64(7) {
		t.Fatalf("EvaluateNode() = %#v, want 7", value)
	}
}

func TestEvaluateMarbleAggregatorFallsBackWhenRemoteAggregateFails(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
		TenantDataReader: aggregateTestTenantDataReader{
			aggregateErr: errors.New("remote aggregate failed"),
			records: []ports.TenantRecord{
				{ObjectID: "txn-1", ObjectType: "transactions", Fields: map[string]any{"amount": float64(10)}},
				{ObjectID: "txn-2", ObjectType: "transactions", Fields: map[string]any{"amount": float64(20)}},
			},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != float64(2) {
		t.Fatalf("EvaluateNode() = %#v, want 2", value)
	}
}

func TestEvaluateMarbleAggregatorFallbackSupportsGroupedFilters(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "and",
				Children: []domainast.Node{
					{
						Function: "Filter",
						NamedChildren: map[string]domainast.Node{
							"tableName": {Constant: "transactions"},
							"fieldName": {Constant: "owner_id"},
							"operator":  {Constant: "="},
							"value":     {Constant: "customer-1"},
						},
					},
					{
						Function: "or",
						Children: []domainast.Node{
							{
								Function: "Filter",
								NamedChildren: map[string]domainast.Node{
									"tableName": {Constant: "transactions"},
									"fieldName": {Constant: "status"},
									"operator":  {Constant: "="},
									"value":     {Constant: "review"},
								},
							},
							{
								Function: "Filter",
								NamedChildren: map[string]domainast.Node{
									"tableName": {Constant: "transactions"},
									"fieldName": {Constant: "status"},
									"operator":  {Constant: "="},
									"value":     {Constant: "decline"},
								},
							},
						},
					},
				},
			},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{},
		TenantDataReader: aggregateTestTenantDataReader{
			aggregateErr: errors.New("remote aggregate failed"),
			records: []ports.TenantRecord{
				{ObjectID: "txn-1", ObjectType: "transactions", Fields: map[string]any{"amount": float64(10), "owner_id": "customer-1", "status": "review"}},
				{ObjectID: "txn-2", ObjectType: "transactions", Fields: map[string]any{"amount": float64(20), "owner_id": "customer-1", "status": "decline"}},
				{ObjectID: "txn-3", ObjectType: "transactions", Fields: map[string]any{"amount": float64(30), "owner_id": "customer-1", "status": "approved"}},
				{ObjectID: "txn-4", ObjectType: "transactions", Fields: map[string]any{"amount": float64(40), "owner_id": "other", "status": "review"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != float64(2) {
		t.Fatalf("EvaluateNode() = %#v, want 2", value)
	}
}

func TestEvaluateMarbleAggregatorStrictModeRejectsUnsupportedPushdown(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
			"filters": {
				Function: "Filter",
				NamedChildren: map[string]domainast.Node{
					"tableName": {Constant: "transactions"},
					"fieldName": {Constant: "status"},
					"operator":  {Constant: "IsEmpty"},
				},
			},
		},
	}

	_, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:               "tenant-1",
		ObjectID:               "txn-1",
		ObjectType:             "transactions",
		Fields:                 map[string]any{},
		TenantDataReader:       aggregateTestTenantDataReader{},
		AggregatePushdownMode:  AggregatePushdownModeStrict,
	})
	if err == nil {
		t.Fatalf("EvaluateNode() error = nil, want strict pushdown error")
	}
}

func TestEvaluateMarbleAggregatorStrictModeRejectsRemoteFailure(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "amount"},
			"aggregator": {Constant: "COUNT"},
		},
	}

	_, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:              "tenant-1",
		ObjectID:              "txn-1",
		ObjectType:            "transactions",
		Fields:                map[string]any{},
		TenantDataReader:      aggregateTestTenantDataReader{aggregateErr: errors.New("boom")},
		AggregatePushdownMode: AggregatePushdownModeStrict,
	})
	if err == nil {
		t.Fatalf("EvaluateNode() error = nil, want strict pushdown failure")
	}
}
