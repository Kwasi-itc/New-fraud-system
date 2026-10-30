package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/jackc/pgx/v5"
)

// Characterization only: capture SQL then stop before database execution. These
// tests do not establish PostgreSQL/MySQL codec, collation or numeric parity.
type sourceReadCapture struct {
	fakeAggregateExecutor
	stopped error
}

func (f *sourceReadCapture) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	f.lastSQL = q
	f.lastArgs = args
	return nil, f.stopped
}
func sourceReadModel() ingestion.PublishedDataModel {
	return ingestion.PublishedDataModel{SchemaName: "source", RecordLookupField: "object_id", Tables: map[string]ingestion.ObjectSchema{"transactions": {Name: "transactions", Fields: map[string]ingestion.FieldSchema{"amount": {Name: "amount"}, "status": {Name: "status"}}}}}
}
func TestSourceRecordReadShapes(t *testing.T) {
	model := sourceReadModel()
	stop := errors.New("captured before execution")
	for _, tc := range []struct {
		name, query string
		args        []any
		call        func(TenantDataReader) error
	}{
		{"get", `SELECT * FROM "source"."transactions" WHERE "object_id" = $1 LIMIT 1`, []any{"one"}, func(r TenantDataReader) error {
			_, e := r.GetRecord(t.Context(), model, "transactions", "one")
			return e
		}},
		{"default_list", `SELECT * FROM "source"."transactions" ORDER BY "updated_at" DESC LIMIT $1`, []any{100}, func(r TenantDataReader) error {
			_, e := r.ListRecords(t.Context(), model, "transactions", -1)
			return e
		}},
		{"large_list", `SELECT * FROM "source"."transactions" ORDER BY "updated_at" DESC LIMIT $1`, []any{100001}, func(r TenantDataReader) error {
			_, e := r.ListRecords(t.Context(), model, "transactions", 100001)
			return e
		}},
		{"search", `SELECT * FROM "source"."transactions" WHERE "amount" = $1 ORDER BY "updated_at" DESC LIMIT $2`, []any{"1.5", 100}, func(r TenantDataReader) error {
			_, e := r.QueryRecords(t.Context(), model, "transactions", "amount", "1.5", 0)
			return e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &sourceReadCapture{stopped: stop}
			err := tc.call(TenantDataReader{db: capture})
			if !errors.Is(err, stop) || capture.lastSQL != tc.query || !reflect.DeepEqual(capture.lastArgs, tc.args) {
				t.Fatal(capture.lastSQL, capture.lastArgs, err)
			}
		})
	}
}
func TestSourcePredicateEdgeSemantics(t *testing.T) {
	model := sourceReadModel()
	table := model.Tables["transactions"]
	for _, tc := range []struct {
		name, op string
		value    any
		sql      string
		args     []any
		bad      bool
	}{
		{"null_equality", "eq", nil, `"status" = $1`, []any{nil}, false},
		{"wildcard_prefix", "starts_with", "a_%", `"status" LIKE ($1 || '%')`, []any{"a_%"}, false},
		{"null_member", "in", []any{"ok", nil}, `"status" IN ($1, $2)`, []any{"ok", nil}, false},
		{"empty_list", "in", []any{}, "", nil, true},
		{"typed_list", "in", []string{"ok"}, "", nil, true},
		{"null_check_ignores_value", "is_null", "ignored", `"status" IS NULL`, []any{}, false},
		{"not_in_unsupported", "not_in", []any{"ok"}, "", nil, true},
		{"regex_unsupported", "regex", "ok.*", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []any{}
			got, err := buildAggregatePredicateSQL(model, table, ingestion.AggregateFilter{Kind: "predicate", Field: "status", Op: tc.op, Value: tc.value}, &args)
			if (err != nil) != tc.bad {
				t.Fatal(got, args, err)
			}
			if !tc.bad && (got != tc.sql || !reflect.DeepEqual(args, tc.args)) {
				t.Fatal(got, args)
			}
		})
	}
}
func TestSourceEmptyGroupsAndArgumentOrder(t *testing.T) {
	model := sourceReadModel()
	table := model.Tables["transactions"]
	for _, filter := range []ingestion.AggregateFilter{{Kind: "group", Operator: "or"}, {Kind: "group", Operator: "not", Children: []ingestion.AggregateFilter{{Kind: "group", Operator: "or"}}}} {
		args := []any{}
		q, err := buildAggregateFilterSQL(model, table, filter, &args)
		if err != nil || q != "" || len(args) != 0 {
			t.Fatal(q, args, err)
		}
	}
	filter := ingestion.AggregateFilter{Children: []ingestion.AggregateFilter{{Kind: "predicate", Field: "status", Op: "eq", Value: "first"}, {Kind: "group", Operator: "not", Children: []ingestion.AggregateFilter{{Kind: "predicate", Field: "amount", Op: "gt", Value: 10}}}}}
	args := []any{}
	q, err := buildAggregateFilterSQL(model, table, filter, &args)
	if err != nil || q != `("status" = $1) AND (NOT ("amount" > $2))` || !reflect.DeepEqual(args, []any{"first", 10}) {
		t.Fatal(q, args, err)
	}
}
func TestSourceAggregateExpressions(t *testing.T) {
	for name, want := range map[string]string{"count": `COUNT("amount")`, "count_distinct": `COUNT(DISTINCT "amount")`, "sum": `COALESCE(SUM("amount"), 0)`, "avg": `AVG("amount")`, "min": `MIN("amount")`, "max": `MAX("amount")`} {
		got, err := buildAggregateExpression(name, "amount")
		if err != nil || got != want {
			t.Fatal(name, got, err)
		}
	}
	if _, err := buildAggregateExpression("median", "amount"); err == nil {
		t.Fatal("unsupported aggregate")
	}
	// Domain validation and repository validation have different entry-point scope.
	query := ingestion.AggregateQuery{ObjectType: "transactions", Aggregate: "count"}
	if err := ingestion.ValidateAggregateQuery(query); err != nil {
		t.Fatal(err)
	}
	if _, err := (TenantDataReader{}).AggregateRecords(t.Context(), sourceReadModel(), query); err == nil {
		t.Fatal("repository must require field")
	}
}
