package mysql

import (
	"database/sql"
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func scalarFixture(t *testing.T) (*sql.Tx, *RecordProjection, time.Time) {
	t.Helper()
	f := recordFixture(t)
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	err := mysqlstore.Mutation(t.Context(), f.db, func(tx *sql.Tx) error {
		w, err := NewRecordWriter(t.Context(), tx, f.database, f.model.TenantID, f.model, "records", f.version)
		if err != nil {
			return err
		}
		for i, key := range []string{"a", "b", "c", "n"} {
			record := map[string]any{"object_id": key}
			if key != "n" {
				record["number"] = int64(i - 1)
				record["amount"] = float64(i-1) / 2
				record["flag"] = i > 0
			}
			if _, err = w.WriteBatch(t.Context(), []map[string]any{record}, ingestion.ModeCreate, at.Add(time.Duration(i-1)*time.Microsecond), 4096); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	p, err := NewRecordProjection(t.Context(), tx, f.database, f.model.TenantID, f.model, "records", f.version, []string{"object_id", "number", "amount", "flag", "updated_at", "ip"})
	if err != nil {
		t.Fatal(err)
	}
	return tx, p, at
}
func scalarMatches(t *testing.T, tx *sql.Tx, p *RecordProjection, field, op string, value any) []string {
	t.Helper()
	pred, err := p.ScalarPredicate(field, op, value)
	if err != nil {
		t.Fatal(err)
	}
	return predicateMatches(t, tx, p, pred)
}
func predicateMatches(t *testing.T, tx *sql.Tx, p *RecordProjection, pred interface{ SQL() (string, []any) }) []string {
	t.Helper()
	b, err := p.Bounded(RecordResultLimits{FieldBytes: 128, TransferBytes: 1024, RowBytes: 4096, PageBytes: 16384, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	columns, table, args := b.SQL()
	where, params := pred.SQL()
	args = append(args, params...)
	rows, err := tx.QueryContext(t.Context(), "SELECT "+columns+" FROM "+table+" WHERE "+where+" LIMIT 10", args...)
	if err != nil {
		t.Fatal(err)
	}
	records, err := b.Collect(rows)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(records))
	for _, r := range records {
		keys = append(keys, r["object_id"].(string))
	}
	sort.Strings(keys)
	return keys
}
func assertScalarMatches(t *testing.T, tx *sql.Tx, p *RecordProjection, field, op string, value any, want string) {
	t.Helper()
	got := strings.Join(scalarMatches(t, tx, p, field, op, value), ",")
	if got != want {
		t.Fatalf("%s %s: got %q want %q", field, op, got, want)
	}
}
func TestScalarPredicateMappingAndArguments(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	pred, err := p.ScalarPredicate(" number ", " GTE ", int64(0))
	if err != nil {
		t.Fatal(err)
	}
	clause, args := pred.SQL()
	if strings.Contains(clause, "number") || !strings.HasSuffix(clause, " >= ?") || !reflect.DeepEqual(args, []any{int64(0)}) {
		t.Fatal(clause, args)
	}
	args[0] = int64(999)
	_, again := pred.SQL()
	if again[0] != int64(0) {
		t.Fatal("mutable arguments")
	}
	for _, field := range []string{"missing", "number` OR 1=1 --", "Number"} {
		got, err := p.ScalarPredicate(field, "eq", nil)
		if !errors.Is(err, ErrRecordPredicate) || got.clause != "" || len(got.args) != 0 {
			t.Fatal(field, got, err)
		}
	}
	assertScalarMatches(t, tx, p, "number", "gte", int64(0), "b,c")
	var absent *RecordProjection
	if _, err := absent.ScalarPredicate("number", "eq", int64(0)); !errors.Is(err, ErrRecordPredicate) {
		t.Fatal(err)
	}
}
func TestScalarPredicateNumericTruthTables(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, tc := range []struct{ op, want string }{{"eq", "b"}, {"neq", "a,c"}, {"gt", "c"}, {"gte", "b,c"}, {"lt", "a"}, {"lte", "a,b"}} {
		assertScalarMatches(t, tx, p, "number", tc.op, int64(0), tc.want)
		assertScalarMatches(t, tx, p, "amount", tc.op, float64(0), tc.want)
	}
	assertScalarMatches(t, tx, p, "number", "gte", int64(math.MinInt32), "a,b,c")
	assertScalarMatches(t, tx, p, "number", "lte", int64(math.MaxInt32), "a,b,c")
	assertScalarMatches(t, tx, p, "amount", "lte", math.MaxFloat64, "a,b,c")
}
func TestScalarPredicateBooleanAndTime(t *testing.T) {
	tx, p, at := scalarFixture(t)
	assertScalarMatches(t, tx, p, "updated_at", "eq", at.In(time.FixedZone("offset", 3600)), "b")
	for _, tc := range []struct{ op, boolWant, timeWant string }{{"eq", "a", "b"}, {"neq", "b,c", "a,c,n"}, {"gt", "b,c", "c,n"}, {"gte", "a,b,c", "b,c,n"}, {"lt", "", "a"}, {"lte", "a", "a,b"}} {
		assertScalarMatches(t, tx, p, "flag", tc.op, false, tc.boolWant)
		assertScalarMatches(t, tx, p, "updated_at", tc.op, at, tc.timeWant)
	}
}
func TestScalarPredicateNullSemantics(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, field := range []string{"number", "amount", "flag", "updated_at"} {
		for _, op := range []string{"eq", "neq", "gt", "gte", "lt", "lte"} {
			assertScalarMatches(t, tx, p, field, op, nil, "")
		}
	}
	for _, field := range []string{"number", "amount", "flag"} {
		assertScalarMatches(t, tx, p, field, "is_null", []any{"ignored"}, "n")
		assertScalarMatches(t, tx, p, field, "is_not_null", nil, "a,b,c")
	}
	assertScalarMatches(t, tx, p, "object_id", "is_null", nil, "")
	assertScalarMatches(t, tx, p, "updated_at", "is_not_null", nil, "a,b,c,n")
}
func TestScalarPredicateRejectsCoercionAndUnsupported(t *testing.T) {
	_, p, at := scalarFixture(t)
	for _, tc := range []struct {
		field, op string
		value     any
	}{
		{"number", "eq", "0"}, {"number", "eq", float64(0)}, {"number", "eq", int64(math.MaxInt32) + 1}, {"number", "eq", int64(math.MinInt32) - 1},
		{"amount", "eq", int64(0)}, {"amount", "eq", math.NaN()}, {"amount", "eq", math.Inf(1)},
		{"flag", "eq", int64(1)}, {"updated_at", "eq", at.Add(time.Nanosecond)}, {"updated_at", "eq", at.Format(time.RFC3339)},
		{"updated_at", "eq", time.Date(999, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"updated_at", "eq", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"object_id", "eq", 42}, {"object_id", "starts_with", true}, {"number", "in", []any{int64(0)}}, {"number", "eq OR 1=1", int64(0)},
	} {
		pred, err := p.ScalarPredicate(tc.field, tc.op, tc.value)
		if !errors.Is(err, ErrRecordPredicate) || pred.clause != "" || len(pred.args) != 0 {
			t.Fatal(tc, pred, err)
		}
	}
}

func TestStringPredicateContracts(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	assertScalarMatches(t, tx, p, "object_id", "eq", "a", "a")
	assertScalarMatches(t, tx, p, "object_id", "eq", "A", "")
	assertScalarMatches(t, tx, p, "object_id", "neq", "a", "b,c,n")
	assertScalarMatches(t, tx, p, "object_id", "starts_with", "a", "a")
	for _, value := range []string{"%", "_", "!", "a%", "a_", "a!"} {
		assertScalarMatches(t, tx, p, "object_id", "starts_with", value, "")
	}
	assertScalarMatches(t, tx, p, "object_id", "eq", nil, "")
}
