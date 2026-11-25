package postgres

import (
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestBuildAggregateSQLParityWithIngestionContract(t *testing.T) {
	t.Parallel()

	model := ports.TenantModel{
		RecordLookupField: "object_id",
	}
	table := ports.TenantModelTable{
		Name: "transactions",
		Fields: map[string]ports.TenantModelField{
			"merchant_id": {Name: "merchant_id", Type: "string"},
			"amount":      {Name: "amount", Type: "float"},
			"status":      {Name: "status", Type: "string"},
			"created_at":  {Name: "created_at", Type: "timestamp"},
		},
	}

	cases := []struct {
		name     string
		agg      string
		field    string
		filter   ports.AggregateFilter
		wantExpr string
		wantSQL  string
		wantArgs []any
	}{
		{
			name:  "count_with_and_eq_gte",
			agg:   "count",
			field: "object_id",
			filter: ports.AggregateFilter{
				Kind:     "group",
				Operator: "and",
				Children: []ports.AggregateFilter{
					{Kind: "predicate", Field: "merchant_id", Op: "eq", Value: "m1"},
					{Kind: "predicate", Field: "created_at", Op: "gte", Value: "2026-11-18T00:00:00Z"},
				},
			},
			wantExpr: `COUNT("object_id")`,
			wantSQL:  `("merchant_id" = $1) AND ("created_at" >= $2)`,
			wantArgs: []any{"m1", "2026-11-18T00:00:00Z"},
		},
		{
			name:  "sum_with_or_not_in_starts_with_nulls",
			agg:   "sum",
			field: "amount",
			filter: ports.AggregateFilter{
				Kind:     "group",
				Operator: "and",
				Children: []ports.AggregateFilter{
					{Kind: "predicate", Field: "status", Op: "in", Value: []any{"approved", "settled"}},
					{
						Kind:     "group",
						Operator: "or",
						Children: []ports.AggregateFilter{
							{Kind: "predicate", Field: "merchant_id", Op: "starts_with", Value: "m_hot_"},
							{
								Kind:     "group",
								Operator: "not",
								Children: []ports.AggregateFilter{
									{Kind: "predicate", Field: "amount", Op: "is_null"},
								},
							},
						},
					},
				},
			},
			wantExpr: `COALESCE(SUM("amount"), 0)`,
			wantSQL:  `("status" IN ($1, $2)) AND (("merchant_id" LIKE ($3 || '%')) OR (NOT ("amount" IS NULL)))`,
			wantArgs: []any{"approved", "settled", "m_hot_"},
		},
		{
			name:     "avg_neq_is_not_null",
			agg:      "avg",
			field:    "amount",
			filter:   ports.AggregateFilter{Kind: "group", Operator: "and", Children: []ports.AggregateFilter{{Kind: "predicate", Field: "status", Op: "neq", Value: "void"}, {Kind: "predicate", Field: "amount", Op: "is_not_null"}}},
			wantExpr: `AVG("amount")`,
			wantSQL:  `("status" <> $1) AND ("amount" IS NOT NULL)`,
			wantArgs: []any{"void"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := buildAggregateExpression(tc.agg, tc.field)
			if err != nil {
				t.Fatalf("buildAggregateExpression() error = %v", err)
			}
			if expr != tc.wantExpr {
				t.Fatalf("expr = %q, want %q", expr, tc.wantExpr)
			}
			var args []any
			where, err := buildAggregateFilterSQL(model, table, tc.filter, &args)
			if err != nil {
				t.Fatalf("buildAggregateFilterSQL() error = %v", err)
			}
			if where != tc.wantSQL {
				t.Fatalf("where = %q, want %q", where, tc.wantSQL)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
		})
	}
}
