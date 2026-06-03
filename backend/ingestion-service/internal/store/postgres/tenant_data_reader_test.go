package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type fakeAggregateExecutor struct {
	lastSQL  string
	lastArgs []any
	value    any
}

func (f *fakeAggregateExecutor) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeAggregateExecutor) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (f *fakeAggregateExecutor) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.lastSQL = sql
	f.lastArgs = args
	return fakeRow{value: f.value}
}

type fakeRow struct {
	value any
}

func (r fakeRow) Scan(dest ...any) error {
	switch target := dest[0].(type) {
	case *any:
		*target = r.value
	}
	return nil
}

func TestBuildAggregateFilterSQLPreservesOrAndNesting(t *testing.T) {
	t.Parallel()

	model := ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
	}
	table := ingestion.ObjectSchema{
		Name: "transactions",
		Fields: map[string]ingestion.FieldSchema{
			"owner_id": {Name: "owner_id"},
			"status":   {Name: "status"},
			"country":  {Name: "country"},
		},
	}
	filter := ingestion.AggregateFilter{
		Kind:     ingestion.AggregateFilterKindGroup,
		Operator: "and",
		Children: []ingestion.AggregateFilter{
			{
				Kind:  ingestion.AggregateFilterKindPredicate,
				Field: "owner_id",
				Op:    "eq",
				Value: "customer-1",
			},
			{
				Kind:     ingestion.AggregateFilterKindGroup,
				Operator: "or",
				Children: []ingestion.AggregateFilter{
					{Kind: ingestion.AggregateFilterKindPredicate, Field: "status", Op: "eq", Value: "review"},
					{Kind: ingestion.AggregateFilterKindPredicate, Field: "country", Op: "eq", Value: "GH"},
				},
			},
		},
	}

	args := []any{}
	sql, err := buildAggregateFilterSQL(model, table, filter, &args)
	if err != nil {
		t.Fatalf("buildAggregateFilterSQL() error = %v", err)
	}
	if !strings.Contains(sql, "AND") || !strings.Contains(sql, "OR") {
		t.Fatalf("buildAggregateFilterSQL() = %q, want grouped AND/OR", sql)
	}
	if len(args) != 3 {
		t.Fatalf("args len = %d, want 3", len(args))
	}
}

func TestBuildAggregateFilterSQLSupportsNot(t *testing.T) {
	t.Parallel()

	model := ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
	}
	table := ingestion.ObjectSchema{
		Name: "transactions",
		Fields: map[string]ingestion.FieldSchema{
			"status": {Name: "status"},
		},
	}
	filter := ingestion.AggregateFilter{
		Kind:     ingestion.AggregateFilterKindGroup,
		Operator: "not",
		Children: []ingestion.AggregateFilter{
			{Kind: ingestion.AggregateFilterKindPredicate, Field: "status", Op: "eq", Value: "approved"},
		},
	}

	args := []any{}
	sql, err := buildAggregateFilterSQL(model, table, filter, &args)
	if err != nil {
		t.Fatalf("buildAggregateFilterSQL() error = %v", err)
	}
	if !strings.Contains(sql, "NOT") {
		t.Fatalf("buildAggregateFilterSQL() = %q, want NOT clause", sql)
	}
	if len(args) != 1 {
		t.Fatalf("args len = %d, want 1", len(args))
	}
}

func TestBuildAggregatePredicateSQLRejectsUnsupportedOperator(t *testing.T) {
	t.Parallel()

	model := ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
	}
	table := ingestion.ObjectSchema{
		Name: "transactions",
		Fields: map[string]ingestion.FieldSchema{
			"status": {Name: "status"},
		},
	}

	args := []any{}
	_, err := buildAggregatePredicateSQL(model, table, ingestion.AggregateFilter{
		Kind:  ingestion.AggregateFilterKindPredicate,
		Field: "status",
		Op:    "contains_any",
		Value: []any{"review"},
	}, &args)
	if err == nil {
		t.Fatalf("buildAggregatePredicateSQL() error = nil, want unsupported operator error")
	}
}

func TestAggregateRecordsSupportsCount(t *testing.T) {
	t.Parallel()

	db := &fakeAggregateExecutor{value: int64(5)}
	reader := TenantDataReader{db: db}
	value, err := reader.AggregateRecords(t.Context(), ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {Name: "transactions", Fields: map[string]ingestion.FieldSchema{"amount": {Name: "amount"}}},
		},
	}, ingestion.AggregateQuery{ObjectType: "transactions", Aggregate: "count", Field: "amount"})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != int64(5) || !strings.Contains(db.lastSQL, "COUNT") {
		t.Fatalf("AggregateRecords() value/sql = %#v / %q", value, db.lastSQL)
	}
}

func TestAggregateRecordsSupportsSum(t *testing.T) {
	t.Parallel()

	db := &fakeAggregateExecutor{value: float64(42)}
	reader := TenantDataReader{db: db}
	value, err := reader.AggregateRecords(t.Context(), ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {Name: "transactions", Fields: map[string]ingestion.FieldSchema{"amount": {Name: "amount"}}},
		},
	}, ingestion.AggregateQuery{ObjectType: "transactions", Aggregate: "sum", Field: "amount"})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != float64(42) || !strings.Contains(db.lastSQL, "SUM") {
		t.Fatalf("AggregateRecords() value/sql = %#v / %q", value, db.lastSQL)
	}
}

func TestAggregateRecordsSupportsAvg(t *testing.T) {
	t.Parallel()

	db := &fakeAggregateExecutor{value: float64(21)}
	reader := TenantDataReader{db: db}
	value, err := reader.AggregateRecords(t.Context(), ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {Name: "transactions", Fields: map[string]ingestion.FieldSchema{"amount": {Name: "amount"}}},
		},
	}, ingestion.AggregateQuery{ObjectType: "transactions", Aggregate: "avg", Field: "amount"})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != float64(21) || !strings.Contains(db.lastSQL, "AVG") {
		t.Fatalf("AggregateRecords() value/sql = %#v / %q", value, db.lastSQL)
	}
}

func TestAggregateRecordsSupportsMin(t *testing.T) {
	t.Parallel()

	db := &fakeAggregateExecutor{value: float64(3)}
	reader := TenantDataReader{db: db}
	value, err := reader.AggregateRecords(t.Context(), ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {Name: "transactions", Fields: map[string]ingestion.FieldSchema{"amount": {Name: "amount"}}},
		},
	}, ingestion.AggregateQuery{ObjectType: "transactions", Aggregate: "min", Field: "amount"})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != float64(3) || !strings.Contains(db.lastSQL, "MIN") {
		t.Fatalf("AggregateRecords() value/sql = %#v / %q", value, db.lastSQL)
	}
}

func TestAggregateRecordsSupportsMax(t *testing.T) {
	t.Parallel()

	db := &fakeAggregateExecutor{value: float64(99)}
	reader := TenantDataReader{db: db}
	value, err := reader.AggregateRecords(t.Context(), ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {Name: "transactions", Fields: map[string]ingestion.FieldSchema{"amount": {Name: "amount"}}},
		},
	}, ingestion.AggregateQuery{ObjectType: "transactions", Aggregate: "max", Field: "amount"})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != float64(99) || !strings.Contains(db.lastSQL, "MAX") {
		t.Fatalf("AggregateRecords() value/sql = %#v / %q", value, db.lastSQL)
	}
}

func TestAggregateRecordsRejectsUnknownObjectType(t *testing.T) {
	t.Parallel()

	reader := TenantDataReader{}
	_, err := reader.AggregateRecords(t.Context(), ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
		Tables:            map[string]ingestion.ObjectSchema{},
	}, ingestion.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
	})
	if err == nil {
		t.Fatalf("AggregateRecords() error = nil, want object type validation error")
	}
}

func TestAggregateRecordsRejectsUnknownField(t *testing.T) {
	t.Parallel()

	reader := TenantDataReader{}
	_, err := reader.AggregateRecords(t.Context(), ingestion.PublishedDataModel{
		TenantID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RecordLookupField: "object_id",
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ingestion.FieldSchema{
					"status": {Name: "status"},
				},
			},
		},
	}, ingestion.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
	})
	if err == nil {
		t.Fatalf("AggregateRecords() error = nil, want field validation error")
	}
}
