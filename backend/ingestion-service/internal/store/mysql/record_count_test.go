package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestRecordCountMappedTypes(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	// Independent fixture expectations: four rows, three non-null numeric/Boolean
	// values (including zero and false), and four required strings/timestamps.
	for _, tc := range []struct {
		field string
		want  int64
	}{{"object_id", 4}, {"number", 3}, {"amount", 3}, {"flag", 3}, {"updated_at", 4}} {
		got, err := p.CountRecords(t.Context(), tx, tc.field, nil, queryLimits())
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
}
func TestRecordCountFilteredEmptyAndNull(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, tc := range []struct {
		field    string
		filterOp string
		want     int64
	}{{"number", "is_null", 0}, {"object_id", "is_null", 1}, {"flag", "is_not_null", 3}} {
		tree := filterLeaf("number", tc.filterOp, nil)
		got, err := p.CountRecords(t.Context(), tx, tc.field, &tree, queryLimits())
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	missing := filterLeaf("number", "eq", int64(99))
	got, err := p.CountRecords(t.Context(), tx, "number", &missing, queryLimits())
	if err != nil || got != 0 {
		t.Fatal(got, err)
	}
	nested := filterGroup("and", filterLeaf("number", "in", []any{int64(0), nil}), filterGroup("not", filterLeaf("flag", "eq", false)))
	got, err = p.CountRecords(t.Context(), tx, "number", &nested, queryLimits())
	if err != nil || got != 1 {
		t.Fatal(got, err)
	}
	empty := filterGroup("or")
	got, err = p.CountRecords(t.Context(), tx, "object_id", &empty, queryLimits())
	if err != nil || got != 4 {
		t.Fatal(got, err)
	}
	// Empty the disposable fixture inside its existing transaction: empty table
	// must produce a zero-valued aggregate row, not a missing-row error.
	if _, err = tx.ExecContext(t.Context(), "DELETE FROM "+p.table); err != nil {
		t.Fatal(err)
	}
	got, err = p.CountRecords(t.Context(), tx, "object_id", nil, queryLimits())
	if err != nil || got != 0 {
		t.Fatal(got, err)
	}
}
func TestRecordCountCompilationAndBounds(t *testing.T) {
	_, p, _ := scalarFixture(t)
	tree := filterLeaf("number", "in", []any{int64(0), nil})
	query, args, err := p.compileCount(" number ", &tree, queryLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(query, "SELECT COUNT(`") || strings.Contains(query, "COUNT(*)") || strings.Contains(query, " LIMIT ") || strings.Contains(query, "OCTET_LENGTH") || !reflect.DeepEqual(args, []any{int64(0), nil}) {
		t.Fatal(query, args)
	}
	exact := RecordQueryLimits{Parameters: 2, ParameterBytes: 9, SQLBytes: len(query)}
	if _, _, err = p.compileCount("number", &tree, exact); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RecordQueryLimits){func(l *RecordQueryLimits) { l.Parameters-- }, func(l *RecordQueryLimits) { l.ParameterBytes-- }, func(l *RecordQueryLimits) { l.SQLBytes-- }} {
		limits := exact
		change(&limits)
		q, a, err := p.compileCount("number", &tree, limits)
		if !errors.Is(err, ErrRecordQueryLimit) || q != "" || a != nil {
			t.Fatal(q, a, err)
		}
	}
	for _, field := range []string{"", "missing", "Number", "number`) FROM secrets --"} {
		q, a, err := p.compileCount(field, nil, queryLimits())
		if !errors.Is(err, ErrRecordCount) || q != "" || a != nil {
			t.Fatal(field, q, a, err)
		}
	}
	invalid := filterLeaf("number", "eq", "0")
	q, a, err := p.compileCount("number", &invalid, queryLimits())
	if !errors.Is(err, ErrRecordFilter) || q != "" || a != nil {
		t.Fatal(q, a, err)
	}
	args[0] = int64(999)
	_, again, err := p.compileCount("number", &tree, exact)
	if err != nil || !reflect.DeepEqual(again, []any{int64(0), nil}) {
		t.Fatal(again, err)
	}
}
func TestRecordCountDecoding(t *testing.T) {
	for _, tc := range []struct {
		raw  any
		want int64
	}{{int64(0), 0}, {int64(math.MaxInt64), math.MaxInt64}, {uint64(7), 7}, {[]byte("9223372036854775807"), math.MaxInt64}} {
		got, err := decodeRecordCount(tc.raw)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	for _, raw := range []any{nil, int64(-1), uint64(math.MaxUint64), []byte("9223372036854775808"), []byte("-1"), []byte(strings.Repeat("9", 21)), []byte("1.5"), float64(1), "1", true} {
		got, err := decodeRecordCount(raw)
		if !errors.Is(err, ErrRecordCountResult) || got != 0 {
			t.Fatal(raw, got, err)
		}
	}
}
func TestRecordCountErrorsAreNotZeroSuccess(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := p.CountRecords(ctx, tx, "number", nil, queryLimits())
	if got != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	limits := queryLimits()
	limits.SQLBytes = 1
	got, err = p.CountRecords(t.Context(), tx, "number", nil, limits)
	if got != 0 || !errors.Is(err, ErrRecordQueryLimit) {
		t.Fatal(got, err)
	}
	got, err = p.CountRecords(t.Context(), tx, "number", nil, queryLimits())
	if got != 0 || !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(got, err)
	}
	if _, err = p.CountRecords(t.Context(), nil, "number", nil, queryLimits()); !errors.Is(err, ErrRecordCount) {
		t.Fatal(err)
	}
	var absent *RecordProjection
	if _, _, err = absent.compileCount("number", nil, queryLimits()); !errors.Is(err, ErrRecordCount) {
		t.Fatal(err)
	}
}
