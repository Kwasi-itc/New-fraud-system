package ast_eval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestAggregateCountDecisionUsesHTTPPushdown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tenants/tenant-1/query/aggregate" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": int64(12)})
	}))
	defer server.Close()

	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "COUNT"},
				},
			},
			{Constant: 10},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Fields:                      map[string]any{},
		TenantDataReader:            ingestionclient.NewHTTPClient(server.URL, time.Second),
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"count"},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != true {
		t.Fatalf("EvaluateNode() = %#v, want true", value)
	}
}

func TestAggregateSumDecisionUsesHTTPPushdown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"value": float64(1450)})
	}))
	defer server.Close()

	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "SUM"},
				},
			},
			{Constant: 1000},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Fields:                      map[string]any{},
		TenantDataReader:            ingestionclient.NewHTTPClient(server.URL, time.Second),
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"count", "sum"},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != true {
		t.Fatalf("EvaluateNode() = %#v, want true", value)
	}
}

func TestAggregateHTTPPushdownPreservesNestedORFilters(t *testing.T) {
	t.Parallel()

	var got ports.AggregateQuery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": int64(3)})
	}))
	defer server.Close()

	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
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
											"fieldName": {Constant: "country"},
											"operator":  {Constant: "="},
											"value":     {Constant: "GH"},
										},
									},
								},
							},
						},
					},
				},
			},
			{Constant: 1},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Fields:                      map[string]any{},
		TenantDataReader:            ingestionclient.NewHTTPClient(server.URL, time.Second),
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"count"},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != true {
		t.Fatalf("EvaluateNode() = %#v, want true", value)
	}
	if got.Filter == nil || got.Filter.Operator != "and" || len(got.Filter.Children) != 2 {
		t.Fatalf("got filter = %#v, want top-level AND with 2 children", got.Filter)
	}
	if got.Filter.Children[1].Operator != "or" || len(got.Filter.Children[1].Children) != 2 {
		t.Fatalf("nested filter = %#v, want preserved OR group", got.Filter.Children[1])
	}
}

func TestAggregateHTTPPushdownResolvesTimeWindowFilters(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	var got ports.AggregateQuery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": int64(2)})
	}))
	defer server.Close()

	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
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
			},
			{Constant: 1},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Now:                         now,
		Fields:                      map[string]any{"created_at": now.Format(time.RFC3339)},
		TenantDataReader:            ingestionclient.NewHTTPClient(server.URL, time.Second),
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"count"},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != true {
		t.Fatalf("EvaluateNode() = %#v, want true", value)
	}
	if got.Filter == nil || len(got.Filter.Children) != 1 {
		t.Fatalf("got filter = %#v, want single child", got.Filter)
	}
	if gotValue, ok := got.Filter.Children[0].Value.(string); !ok {
		t.Fatalf("got filter value type = %T, want string", got.Filter.Children[0].Value)
	} else if gotValue != now.Add(-24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("got filter value = %q, want %q", gotValue, now.Add(-24*time.Hour).Format(time.RFC3339))
	}
}

func TestAggregateHTTPPushdownHandlesLargeDatasetScenario(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"value": int64(50000)})
	}))
	defer server.Close()

	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "COUNT"},
				},
			},
			{Constant: 40000},
		},
	}

	value, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Fields:                      map[string]any{},
		TenantDataReader:            ingestionclient.NewHTTPClient(server.URL, time.Second),
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"count"},
	})
	if err != nil {
		t.Fatalf("EvaluateNode() error = %v", err)
	}
	if value != true {
		t.Fatalf("EvaluateNode() = %#v, want true", value)
	}
}

func TestAggregatePushdownMatchesFallbackOnSupportedRule(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
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
			},
			{Constant: 1},
		},
	}

	records := []ports.TenantRecord{
		{ObjectID: "txn-1", ObjectType: "transactions", Fields: map[string]any{"amount": float64(10), "owner_id": "customer-1", "status": "review"}},
		{ObjectID: "txn-2", ObjectType: "transactions", Fields: map[string]any{"amount": float64(20), "owner_id": "customer-1", "status": "decline"}},
		{ObjectID: "txn-3", ObjectType: "transactions", Fields: map[string]any{"amount": float64(30), "owner_id": "customer-1", "status": "approved"}},
		{ObjectID: "txn-4", ObjectType: "transactions", Fields: map[string]any{"amount": float64(40), "owner_id": "other", "status": "review"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"value": int64(2)})
	}))
	defer server.Close()

	pushdownValue, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectID:                    "txn-1",
		ObjectType:                  "transactions",
		Fields:                      map[string]any{},
		TenantDataReader:            ingestionclient.NewHTTPClient(server.URL, time.Second),
		AggregatePushdownMode:       AggregatePushdownModeEnabled,
		AggregatePushdownAggregates: []string{"count"},
	})
	if err != nil {
		t.Fatalf("pushdown EvaluateNode() error = %v", err)
	}

	fallbackValue, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:              "tenant-1",
		ObjectID:              "txn-1",
		ObjectType:            "transactions",
		Fields:                map[string]any{},
		TenantDataReader:      aggregateTestTenantDataReader{aggregateErr: context.DeadlineExceeded, records: records},
		AggregatePushdownMode: AggregatePushdownModeEnabled,
	})
	if err != nil {
		t.Fatalf("fallback EvaluateNode() error = %v", err)
	}

	if pushdownValue != fallbackValue {
		t.Fatalf("pushdown result = %#v, fallback result = %#v", pushdownValue, fallbackValue)
	}
}
