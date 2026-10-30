package mysql

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func TestRecordMinCompilation(t *testing.T)       { assertExtremeCompilation(t, "MIN") }
func TestRecordMinTypedBoundaries(t *testing.T)   { assertExtremeTyped(t, "MIN") }
func TestRecordMinNullsAndFilters(t *testing.T)   { assertExtremeNulls(t, "MIN") }
func TestRecordMinDecoding(t *testing.T)          { assertExtremeDecoding(t, "MIN") }
func TestRecordMinExecutionFailures(t *testing.T) { assertExtremeErrors(t, "MIN") }
func TestRecordMaxCompilation(t *testing.T)       { assertExtremeCompilation(t, "MAX") }
func TestRecordMaxTypedBoundaries(t *testing.T)   { assertExtremeTyped(t, "MAX") }
func TestRecordMaxNullsAndFilters(t *testing.T)   { assertExtremeNulls(t, "MAX") }
func TestRecordMaxDecoding(t *testing.T)          { assertExtremeDecoding(t, "MAX") }
func TestRecordMaxExecutionFailures(t *testing.T) { assertExtremeErrors(t, "MAX") }

func assertExtremeCompilation(t *testing.T, op string) {
	t.Helper()
	_, p, _ := scalarFixture(t)
	assertAggregateCompilation(t, p, op, []string{"flag", "ip"})
}
func assertExtremeTyped(t *testing.T, op string) {
	t.Helper()
	tx, p, _ := scalarFixture(t)
	call := p.MinRecords
	if op == "MAX" {
		call = p.MaxRecords
	}
	number, amount, stamp := sumTestColumn(t, p, "number"), sumTestColumn(t, p, "amount"), sumTestColumn(t, p, "updated_at")
	earliest := time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)
	latest := time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)
	if _, err := tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+amount+"=CASE WHEN "+number+"<0 THEN ? WHEN "+number+">0 THEN ? ELSE "+amount+" END, "+stamp+"=CASE WHEN "+number+"<0 THEN ? WHEN "+number+">0 THEN ? ELSE "+stamp+" END", -math.MaxFloat64, math.MaxFloat64, earliest, latest); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+number+"=CASE WHEN "+number+"<0 THEN ? WHEN "+number+">0 THEN ? ELSE "+number+" END", int64(math.MinInt32), int64(math.MaxInt32)); err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{"number": int64(math.MinInt32), "amount": -math.MaxFloat64, "updated_at": earliest}
	if op == "MAX" {
		wants = map[string]any{"number": int64(math.MaxInt32), "amount": math.MaxFloat64, "updated_at": latest}
	}
	for field, want := range wants {
		got, err := call(t.Context(), tx, field, nil, queryLimits())
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(op, field, got, want, err)
		}
	}
}
func assertExtremeNulls(t *testing.T, op string) {
	t.Helper()
	tx, p, at := scalarFixture(t)
	call := p.MinRecords
	if op == "MAX" {
		call = p.MaxRecords
	}
	for _, field := range []string{"number", "amount"} {
		allNull := filterLeaf("number", "is_null", nil)
		got, err := call(t.Context(), tx, field, &allNull, queryLimits())
		if err != nil || got != nil {
			t.Fatal(op, field, got, err)
		}
	}
	empty := filterLeaf("number", "eq", int64(99))
	got, err := call(t.Context(), tx, "updated_at", &empty, queryLimits())
	if err != nil || got != nil {
		t.Fatal(got, err)
	}
	nested := filterGroup("and", filterLeaf("number", "in", []any{int64(0), nil}), filterGroup("not", filterLeaf("flag", "eq", false)))
	got, err = call(t.Context(), tx, "updated_at", &nested, queryLimits())
	if err != nil || !reflect.DeepEqual(got, at) {
		t.Fatal(got, err)
	}
	emptyOr := filterGroup("or")
	got, err = call(t.Context(), tx, "number", &emptyOr, queryLimits())
	want := int64(-1)
	if op == "MAX" {
		want = 1
	}
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	if _, err = tx.ExecContext(t.Context(), "DELETE FROM "+p.table); err != nil {
		t.Fatal(err)
	}
	got, err = call(t.Context(), tx, "updated_at", nil, queryLimits())
	if err != nil || got != nil {
		t.Fatal(got, err)
	}
}
func assertExtremeDecoding(t *testing.T, op string) {
	t.Helper()
	at := time.Date(2026, 10, 29, 12, 0, 0, 123456000, time.UTC)
	for _, tc := range []struct {
		kind      string
		raw, want any
	}{
		{"int", []byte("-2147483648"), int64(math.MinInt32)},
		{"float", float64(-0.5), float64(-0.5)},
		{"timestamp", at.In(time.FixedZone("offset", 3600)), at},
	} {
		got, err := decodeRecordExtreme(ingestion.FieldSchema{DataType: tc.kind}, tc.raw, int64(1))
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(op, got, err)
		}
	}
	for _, tc := range []struct {
		kind       string
		raw, count any
	}{
		{"int", nil, int64(1)}, {"int", int64(1), int64(0)}, {"int", int64(math.MaxInt32) + 1, int64(1)},
		{"float", math.NaN(), int64(1)}, {"float", math.Inf(-1), int64(1)},
		{"timestamp", at.Add(time.Nanosecond), int64(1)}, {"timestamp", "2026-10-29", int64(1)},
		{"int", int64(0), int64(-1)}, {"int", int64(0), nil}, {"bool", int64(1), int64(1)},
	} {
		got, err := decodeRecordExtreme(ingestion.FieldSchema{DataType: tc.kind, Nullable: true}, tc.raw, tc.count)
		if !errors.Is(err, ErrRecordExtremeResult) || got != nil {
			t.Fatal(op, tc, got, err)
		}
	}
	if got, err := decodeRecordExtreme(ingestion.FieldSchema{DataType: "int"}, nil, int64(0)); err != nil || got != nil {
		t.Fatal(got, err)
	}
	if _, err := decodeRecordExtreme(ingestion.FieldSchema{DataType: "int", Archived: true}, int64(1), int64(1)); !errors.Is(err, ErrRecordExtremeResult) {
		t.Fatal(err)
	}
}
func assertExtremeErrors(t *testing.T, op string) {
	t.Helper()
	tx, p, _ := scalarFixture(t)
	call := p.MinRecords
	if op == "MAX" {
		call = p.MaxRecords
	}
	assertNullableAggregateErrors(t, tx, call)
}
