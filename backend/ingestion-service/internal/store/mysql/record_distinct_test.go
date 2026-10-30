package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func TestRecordDistinctCompilation(t *testing.T) {
	_, p, _ := scalarFixture(t)
	assertAggregateCompilation(t, p, "COUNT DISTINCT", nil)
	q, _, _, err := p.compileAggregate("flag", "COUNT DISTINCT", nil, queryLimits())
	if err != nil || !strings.HasPrefix(q, "SELECT COUNT(DISTINCT `") || strings.Contains(q, "CAST(") {
		t.Fatal(q, err)
	}
}
func TestRecordDistinctNumericEquality(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, field := range []string{"number", "amount"} {
		got, err := p.CountDistinctRecords(t.Context(), tx, field, nil, queryLimits())
		if err != nil || got != 3 {
			t.Fatal(field, got, err)
		}
	}
	amount, number := sumTestColumn(t, p, "amount"), sumTestColumn(t, p, "number")
	if _, err := tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+amount+"=CASE WHEN "+number+"=0 THEN ? WHEN "+number+" IS NOT NULL THEN ? ELSE NULL END", math.Nextafter(1, 2), float64(1)); err != nil {
		t.Fatal(err)
	}
	got, err := p.CountDistinctRecords(t.Context(), tx, "amount", nil, queryLimits())
	if err != nil || got != 2 {
		t.Fatal("adjacent doubles collapsed", got, err)
	}
	if _, err = tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+amount+"=CASE WHEN "+number+"=0 THEN ? WHEN "+number+" IS NOT NULL THEN ? ELSE NULL END", math.Copysign(0, -1), float64(0)); err != nil {
		t.Fatal(err)
	}
	got, err = p.CountDistinctRecords(t.Context(), tx, "amount", nil, queryLimits())
	if err != nil || got != 1 {
		t.Fatal("signed zero distinctness", got, err)
	}
}
func TestRecordDistinctBooleanTimeAndNull(t *testing.T) {
	tx, p, at := scalarFixture(t)
	got, err := p.CountDistinctRecords(t.Context(), tx, "flag", nil, queryLimits())
	if err != nil || got != 2 {
		t.Fatal(got, err)
	}
	got, err = p.CountDistinctRecords(t.Context(), tx, "updated_at", nil, queryLimits())
	if err != nil || got != 4 {
		t.Fatal("microseconds collapsed", got, err)
	}
	stamp, number := sumTestColumn(t, p, "updated_at"), sumTestColumn(t, p, "number")
	if _, err = tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+stamp+"=? WHERE "+number+">=0", at); err != nil {
		t.Fatal(err)
	}
	got, err = p.CountDistinctRecords(t.Context(), tx, "updated_at", nil, queryLimits())
	if err != nil || got != 3 {
		t.Fatal(got, err)
	}
	nulls := filterLeaf("number", "is_null", nil)
	got, err = p.CountDistinctRecords(t.Context(), tx, "flag", &nulls, queryLimits())
	if err != nil || got != 0 {
		t.Fatal(got, err)
	}
	if _, err = tx.ExecContext(t.Context(), "DELETE FROM "+p.table); err != nil {
		t.Fatal(err)
	}
	got, err = p.CountDistinctRecords(t.Context(), tx, "updated_at", nil, queryLimits())
	if err != nil || got != 0 {
		t.Fatal(got, err)
	}
}
func TestRecordDistinctFilteredCountAndDecoder(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	tree := filterGroup("and", filterLeaf("number", "in", []any{int64(0), int64(1), nil}), filterGroup("not", filterLeaf("flag", "eq", false)))
	got, err := p.CountDistinctRecords(t.Context(), tx, "flag", &tree, queryLimits())
	if err != nil || got != 1 {
		t.Fatal(got, err)
	}
	for _, tree := range []ingestion.AggregateFilter{filterGroup("or"), filterLeaf("number", "eq", int64(99))} {
		want := int64(2)
		if tree.Kind == "predicate" {
			want = 0
		}
		got, err = p.CountDistinctRecords(t.Context(), tx, "flag", &tree, queryLimits())
		if err != nil || got != want {
			t.Fatal(got, err)
		}
	}
	for _, raw := range []any{nil, int64(-1), uint64(math.MaxUint64), []byte("9223372036854775808"), float64(1)} {
		if _, err := decodeRecordCount(raw); !errors.Is(err, ErrRecordCountResult) {
			t.Fatal(raw, err)
		}
	}
	if value, err := decodeRecordCount([]byte("9223372036854775807")); err != nil || value != math.MaxInt64 {
		t.Fatal(value, err)
	}
}
func TestRecordDistinctExecutionFailures(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := p.CountDistinctRecords(ctx, tx, "number", nil, queryLimits())
	if got != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	limits := queryLimits()
	limits.SQLBytes = 1
	got, err = p.CountDistinctRecords(t.Context(), tx, "number", nil, limits)
	if got != 0 || !errors.Is(err, ErrRecordQueryLimit) {
		t.Fatal(got, err)
	}
	got, err = p.CountDistinctRecords(t.Context(), tx, "number", nil, queryLimits())
	if got != 0 || !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(got, err)
	}
	if _, err = p.CountDistinctRecords(t.Context(), nil, "number", nil, queryLimits()); !errors.Is(err, ErrRecordAggregate) {
		t.Fatal(err)
	}
}
