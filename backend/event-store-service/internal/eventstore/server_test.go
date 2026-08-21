package eventstore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventAggregateRequiresUsableTimeLowerBound(t *testing.T) {
	request := AggregateRequest{Table: testTableContract(), Field: "amount"}
	if err := validateAggregateRequest(request); err == nil {
		t.Fatal("expected unbounded aggregate to be rejected")
	}
	request.Filter = &AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-01-01T00:00:00Z"}
	if err := validateAggregateRequest(request); err != nil {
		t.Fatalf("bounded aggregate rejected: %v", err)
	}
	request.Filter = &AggregateFilter{Kind: "group", Operator: "or", Children: []AggregateFilter{{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-01-01T00:00:00Z"}, {Kind: "predicate", Field: "country", Op: "eq", Value: "GH"}}}
	if err := validateAggregateRequest(request); err == nil {
		t.Fatal("OR filter must not be treated as a global bound")
	}
}

func TestBuildInsertRowStoresDataModelFieldsAsTypedColumns(t *testing.T) {
	table := testTableContract()
	table.Fields["account_ref"] = FieldContract{DataType: "string"}
	table.Fields["merchant_id"] = FieldContract{DataType: "string"}
	eventTime := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	row, err := buildInsertRow(table, Event{
		EventID:     "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		ObjectID:    "txn-1",
		EventTime:   eventTime,
		IngestedAt:  eventTime.Add(time.Second),
		RequestHash: "request-hash",
		Payload: map[string]any{
			"object_id":   "txn-1",
			"date":        eventTime,
			"amount":      json.Number("12500.50"),
			"country":     "GH",
			"account_ref": "acct-1",
			"merchant_id": "merchant-1",
		},
	})
	if err != nil {
		t.Fatalf("buildInsertRow() error = %v", err)
	}
	for _, name := range []string{"account_ref", "merchant_id", "amount", "date", "object_id"} {
		if _, ok := row[name]; !ok {
			t.Fatalf("typed row missing data-model column %s: %#v", name, row)
		}
	}
	if _, ok := row["payload"]; ok {
		t.Fatalf("typed row must not contain a JSON payload column: %#v", row)
	}
}

func TestFieldExpressionUsesPhysicalColumn(t *testing.T) {
	expression, err := fieldExpression("amount", testTableContract().Fields)
	if err != nil {
		t.Fatalf("fieldExpression() error = %v", err)
	}
	if expression != "`amount`" || strings.Contains(expression, "JSON") {
		t.Fatalf("fieldExpression() = %q, want direct typed column", expression)
	}
}

func TestEventTimeFilterUsesPhysicalSortColumn(t *testing.T) {
	filter := AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-01-01T00:00:00Z"}
	query, err := buildFilterSQL(filter, testTableContract().Fields)
	if err != nil {
		t.Fatalf("buildFilterSQL returned error: %v", err)
	}
	if !strings.HasPrefix(query, "`date` >=") {
		t.Fatalf("expected partition-prunable typed date predicate, got %q", query)
	}
}

func TestBuildFilterSQLRejectsNotWithEmptyChild(t *testing.T) {
	filter := AggregateFilter{
		Kind:     "group",
		Operator: "not",
		Children: []AggregateFilter{{Kind: "group", Operator: "and"}},
	}
	if _, err := buildFilterSQL(filter, testTableContract().Fields); err == nil {
		t.Fatal("expected malformed NOT filter to be rejected")
	}
}

func testTableContract() TableContract {
	return TableContract{
		TenantID:       "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		TableID:        "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ObjectType:     "transactions",
		RevisionID:     "revision",
		SchemaRevision: "schema-revision",
		EventTimeField: "date",
		Fields: map[string]FieldContract{
			"object_id": {DataType: "string"},
			"date":      {DataType: "timestamp"},
			"amount":    {DataType: "float"},
			"country":   {DataType: "string"},
		},
	}
}
