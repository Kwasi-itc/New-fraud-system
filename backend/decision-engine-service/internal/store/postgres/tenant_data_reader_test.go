package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type fakeTenantModelReader struct {
	model ports.TenantModel
	err   error
}

func (f fakeTenantModelReader) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return f.model, f.err
}

func (f fakeTenantModelReader) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, nil
}

func (f fakeTenantModelReader) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, nil
}

func (f fakeTenantModelReader) RetryIndexJob(context.Context, string) error {
	return nil
}

type fakeAggregateExecutor struct {
	lastSQL  string
	lastArgs []any
	value    any
	rows     *fakeRows
}

func (f *fakeAggregateExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if f.rows == nil {
		return &fakeRows{}, nil
	}
	return f.rows, nil
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

type fakeRows struct {
	items [][]byte
	index int
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Next() bool {
	if r.index >= len(r.items) {
		return false
	}
	r.index++
	return true
}
func (r *fakeRows) Scan(dest ...any) error {
	target := dest[0].(*[]byte)
	*target = append((*target)[:0], r.items[r.index-1]...)
	return nil
}
func (r *fakeRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRows) RawValues() [][]byte    { return nil }
func (r *fakeRows) Conn() *pgx.Conn        { return nil }

func directReadModel() ports.TenantModel {
	return ports.TenantModel{
		RecordLookupField: "object_id",
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"amount": {Name: "amount"},
					"status": {Name: "status"},
				},
			},
		},
	}
}

func TestTenantSchemaNameNormalizesTenantID(t *testing.T) {
	t.Parallel()

	got, err := tenantSchemaName("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("tenantSchemaName() error = %v", err)
	}
	if got != "tenant_11111111111111111111111111111111" {
		t.Fatalf("tenantSchemaName() = %q", got)
	}
}

func TestBuildAggregatePredicateSQLUsesPlaceholders(t *testing.T) {
	t.Parallel()

	model := directReadModel()
	args := []any{}
	sql, err := buildAggregatePredicateSQL(model, model.Tables["transactions"], ports.AggregateFilter{
		Kind:  "predicate",
		Field: "status",
		Op:    "eq",
		Value: "review",
	}, &args)
	if err != nil {
		t.Fatalf("buildAggregatePredicateSQL() error = %v", err)
	}
	if !strings.Contains(sql, "$1") {
		t.Fatalf("buildAggregatePredicateSQL() = %q, want placeholder", sql)
	}
	if strings.Contains(sql, "review") {
		t.Fatalf("buildAggregatePredicateSQL() = %q, value should not be interpolated", sql)
	}
}

func TestAggregateRecordsUsesDataModelContract(t *testing.T) {
	t.Parallel()

	db := &fakeAggregateExecutor{value: int64(5)}
	reader := NewTenantDataReader(db, fakeTenantModelReader{model: directReadModel()})

	value, err := reader.AggregateRecords(context.Background(), "11111111-1111-1111-1111-111111111111", ports.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
		Filter: &ports.AggregateFilter{
			Kind:  "predicate",
			Field: "status",
			Op:    "eq",
			Value: "review",
		},
	})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != int64(5) {
		t.Fatalf("AggregateRecords() value = %#v, want 5", value)
	}
	if !strings.Contains(db.lastSQL, `COUNT("amount")`) {
		t.Fatalf("AggregateRecords() SQL = %q", db.lastSQL)
	}
	if !strings.Contains(db.lastSQL, `"tenant_11111111111111111111111111111111"."transactions"`) {
		t.Fatalf("AggregateRecords() SQL = %q", db.lastSQL)
	}
	if len(db.lastArgs) != 1 || db.lastArgs[0] != "review" {
		t.Fatalf("AggregateRecords() args = %#v", db.lastArgs)
	}
}

func TestGetRecordUsesTypedDirectReadAdapter(t *testing.T) {
	t.Parallel()

	db := &fakeAggregateExecutor{
		rows: &fakeRows{
			items: [][]byte{[]byte(`{"object_id":"txn-1","amount":42}`)},
		},
	}
	reader := NewTenantDataReader(db, fakeTenantModelReader{model: directReadModel()})

	record, err := reader.GetRecord(context.Background(), "11111111-1111-1111-1111-111111111111", "transactions", "txn-1")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if record.ObjectID != "txn-1" || record.ObjectType != "transactions" {
		t.Fatalf("GetRecord() = %#v", record)
	}
	if got := record.Fields["amount"]; got != float64(42) {
		t.Fatalf("record.Fields[amount] = %#v", got)
	}
}

func TestTenantDataReaderRejectsInvalidTenantID(t *testing.T) {
	t.Parallel()

	reader := NewTenantDataReader(&fakeAggregateExecutor{}, fakeTenantModelReader{model: directReadModel()})
	_, err := reader.AggregateRecords(context.Background(), "not-a-uuid", ports.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
	})
	if err == nil || !strings.Contains(err.Error(), "parse tenant id") {
		t.Fatalf("AggregateRecords() error = %v, want tenant id parse error", err)
	}
}

func TestTenantDataReaderRejectsUnknownObjectType(t *testing.T) {
	t.Parallel()

	reader := NewTenantDataReader(&fakeAggregateExecutor{}, fakeTenantModelReader{model: directReadModel()})
	_, err := reader.AggregateRecords(context.Background(), "11111111-1111-1111-1111-111111111111", ports.AggregateQuery{
		ObjectType: "accounts",
		Aggregate:  "count",
		Field:      "amount",
	})
	if err == nil || !strings.Contains(err.Error(), "object type accounts is not available") {
		t.Fatalf("AggregateRecords() error = %v, want object-type validation error", err)
	}
}

func TestTenantDataReaderRejectsUnknownField(t *testing.T) {
	t.Parallel()

	reader := NewTenantDataReader(&fakeAggregateExecutor{}, fakeTenantModelReader{model: directReadModel()})
	_, err := reader.AggregateRecords(context.Background(), "11111111-1111-1111-1111-111111111111", ports.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "missing_field",
	})
	if err == nil || !strings.Contains(err.Error(), "field missing_field is not available") {
		t.Fatalf("AggregateRecords() error = %v, want field validation error", err)
	}
}
