package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func TestRecordSumCompilation(t *testing.T) {
	_, p, _ := scalarFixture(t)
	tree := filterLeaf("number", "in", []any{int64(0), nil})
	query, args, kind, err := p.compileSum(" number ", &tree, queryLimits())
	if err != nil {
		t.Fatal(err)
	}
	if kind != "int" || !strings.HasPrefix(query, "SELECT SUM(`") || !strings.Contains(query, "), COUNT(`") || strings.Contains(query, " LIMIT ") || !reflect.DeepEqual(args, []any{int64(0), nil}) {
		t.Fatal(query, args, kind)
	}
	exact := RecordQueryLimits{Parameters: 2, ParameterBytes: 9, SQLBytes: len(query)}
	if _, _, _, err = p.compileSum("number", &tree, exact); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RecordQueryLimits){func(l *RecordQueryLimits) { l.Parameters-- }, func(l *RecordQueryLimits) { l.ParameterBytes-- }, func(l *RecordQueryLimits) { l.SQLBytes-- }} {
		limits := exact
		change(&limits)
		q, a, k, err := p.compileSum("number", &tree, limits)
		if !errors.Is(err, ErrRecordQueryLimit) || q != "" || a != nil || k != "" {
			t.Fatal(q, a, k, err)
		}
	}
	for _, field := range []string{"", "missing", "Number", "number`) FROM secrets --", "object_id", "flag", "updated_at", "ip"} {
		q, a, k, err := p.compileSum(field, nil, queryLimits())
		if !errors.Is(err, ErrRecordSum) || q != "" || a != nil || k != "" {
			t.Fatal(field, q, a, k, err)
		}
	}
	args[0] = int64(99)
	_, again, _, err := p.compileSum("number", &tree, exact)
	if err != nil || !reflect.DeepEqual(again, []any{int64(0), nil}) {
		t.Fatal(again, err)
	}
}
func TestRecordSumIntegerPrecision(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	negative := filterLeaf("number", "lt", int64(0))
	got, err := p.SumRecords(t.Context(), tx, "number", &negative, queryLimits())
	if err != nil || got != int64(-1) {
		t.Fatal(got, err)
	}
	// Three distinct valid INT32s near the upper limit produce an exact total
	// larger than INT32, calculated independently: 2147483645+2147483646+2147483647.
	column := sumTestColumn(t, p, "number")
	if _, err = tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+column+"="+column+"+2147483646"); err != nil {
		t.Fatal(err)
	}
	got, err = p.SumRecords(t.Context(), tx, "number", nil, queryLimits())
	if err != nil || got != int64(6442450938) {
		t.Fatal(got, err)
	}
}
func sumTestColumn(t *testing.T, p *RecordProjection, name string) string {
	t.Helper()
	for _, field := range p.fields {
		if field.definition.Name == name {
			return field.column
		}
	}
	t.Fatal("missing fixture field", name)
	return ""
}
func TestRecordSumFloatAndEmptyInputs(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	column := sumTestColumn(t, p, "amount")
	number := sumTestColumn(t, p, "number")
	if _, err := tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+column+"=CASE "+number+" WHEN -1 THEN 0.1 WHEN 0 THEN 0.2 WHEN 1 THEN -0.05 ELSE NULL END"); err != nil {
		t.Fatal(err)
	}
	got, err := p.SumRecords(t.Context(), tx, "amount", nil, queryLimits())
	if err != nil {
		t.Fatal(err)
	}
	value, ok := got.(float64)
	if !ok || math.Abs(value-0.25) > 1e-12 {
		t.Fatal(got)
	}
	for _, field := range []string{"number", "amount"} {
		for _, filter := range []ingestion.AggregateFilter{filterLeaf("number", "eq", int64(99)), filterLeaf("number", "is_null", nil)} {
			got, err = p.SumRecords(t.Context(), tx, field, &filter, queryLimits())
			if err != nil {
				t.Fatal(err)
			}
			if field == "number" && got != int64(0) || field == "amount" && got != float64(0) {
				t.Fatal(field, got)
			}
		}
	}
	empty := filterGroup("or")
	got, err = p.SumRecords(t.Context(), tx, "amount", &empty, queryLimits())
	if err != nil || math.Abs(got.(float64)-0.25) > 1e-12 {
		t.Fatal(got, err)
	}
	if _, err = tx.ExecContext(t.Context(), "DELETE FROM "+p.table); err != nil {
		t.Fatal(err)
	}
	got, err = p.SumRecords(t.Context(), tx, "amount", nil, queryLimits())
	if err != nil || got != float64(0) {
		t.Fatal(got, err)
	}
}
func TestRecordSumDecoderBounds(t *testing.T) {
	for _, tc := range []struct {
		kind      string
		raw, want any
	}{
		{"int", []byte("9007199254740993"), int64(9007199254740993)},
		{"int", []byte("9223372036854775807"), int64(math.MaxInt64)},
		{"int", []byte("-9223372036854775808"), int64(math.MinInt64)},
		{"float", float64(0), float64(0)}, {"float", []byte("1.25"), float64(1.25)},
	} {
		got, err := decodeRecordSum(tc.kind, tc.raw)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	for _, tc := range []struct {
		kind string
		raw  any
	}{
		{"int", []byte("9223372036854775808")}, {"int", []byte("-9223372036854775809")}, {"int", float64(1)}, {"int", []byte("1.0")}, {"int", nil},
		{"float", math.NaN()}, {"float", math.Inf(1)}, {"float", []byte("1e999")}, {"float", nil}, {"float", int64(1)}, {"bool", int64(1)},
	} {
		got, err := decodeRecordSum(tc.kind, tc.raw)
		if !errors.Is(err, ErrRecordSumResult) || got != nil {
			t.Fatal(tc, got, err)
		}
	}
}
func TestRecordSumFailuresAndOverflow(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	column := sumTestColumn(t, p, "amount")
	if _, err := tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+column+"=?", math.MaxFloat64); err != nil {
		t.Fatal(err)
	}
	var nonnull, total int64
	var rawSum any
	if err := tx.QueryRowContext(t.Context(), "SELECT COUNT("+column+"), COUNT(*), SUM("+column+") FROM "+p.table).Scan(&nonnull, &total, &rawSum); err != nil {
		t.Fatal(err)
	}
	warnings, err := tx.QueryContext(t.Context(), "SHOW WARNINGS")
	if err != nil {
		t.Fatal(err)
	}
	for warnings.Next() {
		var level, msg string
		var code int
		if err := warnings.Scan(&level, &code, &msg); err != nil {
			t.Fatal(err)
		}
		t.Log(level, code, msg)
	}
	if err := warnings.Close(); err != nil {
		t.Fatal(err)
	}
	var lo, hi float64
	if err := tx.QueryRowContext(t.Context(), "SELECT MIN("+column+"),MAX("+column+") FROM "+p.table).Scan(&lo, &hi); err != nil {
		t.Fatal(err)
	}
	t.Logf("stored min=%g max=%g", lo, hi)
	t.Logf("overflow fixture: nonnull=%d total=%d rawSum=%v type=%T", nonnull, total, rawSum, rawSum)
	got, err := p.SumRecords(t.Context(), tx, "amount", nil, queryLimits())
	if !errors.Is(err, ErrRecordSumResult) || got != nil {
		t.Fatal("overflow became success", got, err)
	}
	// One finite extreme input is safe; conservative protection must not reject it.
	singleton := filterLeaf("number", "eq", int64(0))
	got, err = p.SumRecords(t.Context(), tx, "amount", &singleton, queryLimits())
	if err != nil || got != float64(math.MaxFloat64) {
		t.Fatal(got, err)
	}
	// Opposite extremes can cancel, but are deliberately outside this safe policy.
	number := sumTestColumn(t, p, "number")
	if _, err = tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+column+"=CASE WHEN "+number+"<0 THEN ? ELSE ? END", -math.MaxFloat64, math.MaxFloat64); err != nil {
		t.Fatal(err)
	}
	pair := filterLeaf("number", "in", []any{int64(-1), int64(0)})
	got, err = p.SumRecords(t.Context(), tx, "amount", &pair, queryLimits())
	if !errors.Is(err, ErrRecordSumResult) || got != nil {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err = p.SumRecords(ctx, tx, "number", nil, queryLimits())
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	limits := queryLimits()
	limits.SQLBytes = 1
	got, err = p.SumRecords(t.Context(), tx, "number", nil, limits)
	if got != nil || !errors.Is(err, ErrRecordQueryLimit) {
		t.Fatal(got, err)
	}
	got, err = p.SumRecords(t.Context(), tx, "number", nil, queryLimits())
	if got != nil || !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(got, err)
	}
	if _, err = p.SumRecords(t.Context(), nil, "number", nil, queryLimits()); !errors.Is(err, ErrRecordSum) {
		t.Fatal(err)
	}
	var absent *RecordProjection
	if _, _, _, err = absent.compileSum("number", nil, queryLimits()); !errors.Is(err, ErrRecordSum) {
		t.Fatal(err)
	}
}
