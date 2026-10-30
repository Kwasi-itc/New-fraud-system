package mysql

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func TestRecordAverageCompilation(t *testing.T) {
	_, p, _ := scalarFixture(t)
	assertAggregateCompilation(t, p, "AVG", []string{"object_id", "flag", "updated_at", "ip"})
	q, _, _, err := p.compileAggregate("amount", "AVG", nil, queryLimits())
	if err != nil || !strings.Contains(q, "AVG(") || !strings.Contains(q, "MAX(ABS(") {
		t.Fatal(q, err)
	}
}
func TestRecordAverageIntegerDecimal(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	var increment int
	if err := tx.QueryRowContext(t.Context(), "SELECT @@session.div_precision_increment").Scan(&increment); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture div_precision_increment=%d", increment)
	if increment != 4 {
		t.Fatal("fixture decimal-scale expectation must be reviewed", increment)
	}
	nonnegative := filterLeaf("number", "gte", int64(0))
	got, err := p.AverageRecords(t.Context(), tx, "number", &nonnegative, queryLimits())
	if err != nil || got != RecordDecimal("0.50000000000000000000") {
		t.Fatal(got, err)
	}
	column := sumTestColumn(t, p, "number")
	if _, err = tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+column+"=2 WHERE "+column+"=1"); err != nil {
		t.Fatal(err)
	}
	got, err = p.AverageRecords(t.Context(), tx, "number", nil, queryLimits())
	if err != nil || got != RecordDecimal("0.33333333333333333333") {
		t.Fatal(got, err)
	}
	negative := filterLeaf("number", "lt", int64(0))
	got, err = p.AverageRecords(t.Context(), tx, "number", &negative, queryLimits())
	if err != nil || got != RecordDecimal("-1.00000000000000000000") {
		t.Fatal(got, err)
	}
	// Retain decimal digits as provided; do not introduce binary rounding.
	raw := []byte("0.123456789012345678901234567890")
	decoded, err := decodeRecordAverageDecimal(raw)
	if err != nil || decoded != RecordDecimal(string(raw)) {
		t.Fatal(decoded, err)
	}
	raw[0] = '9'
	if decoded != "0.123456789012345678901234567890" {
		t.Fatal("aliased decimal")
	}
}
func TestRecordAverageFloatAndEmpty(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	positive := filterLeaf("number", "gte", int64(0))
	got, err := p.AverageRecords(t.Context(), tx, "amount", &positive, queryLimits())
	if err != nil || got != float64(0.25) {
		t.Fatal(got, err)
	}
	nested := filterGroup("and", filterLeaf("number", "in", []any{int64(0), int64(1), nil}), filterLeaf("flag", "eq", true))
	got, err = p.AverageRecords(t.Context(), tx, "amount", &nested, queryLimits())
	if err != nil || got != float64(0.25) {
		t.Fatal(got, err)
	}
	for _, field := range []string{"number", "amount"} {
		for _, tree := range []ingestion.AggregateFilter{filterLeaf("number", "eq", int64(99)), filterLeaf("number", "is_null", nil)} {
			got, err = p.AverageRecords(t.Context(), tx, field, &tree, queryLimits())
			if err != nil || got != nil {
				t.Fatal(field, got, err)
			}
		}
	}
	empty := filterGroup("or")
	got, err = p.AverageRecords(t.Context(), tx, "amount", &empty, queryLimits())
	if err != nil || got != float64(0) {
		t.Fatal(got, err)
	}
	if _, err = tx.ExecContext(t.Context(), "DELETE FROM "+p.table); err != nil {
		t.Fatal(err)
	}
	got, err = p.AverageRecords(t.Context(), tx, "number", nil, queryLimits())
	if err != nil || got != nil {
		t.Fatal(got, err)
	}
}
func TestRecordAveragePrecisionAndSafety(t *testing.T) {
	for _, raw := range []any{nil, int64(0), float64(0), "", "-", "+1", ".1", "1.", "1e3", "NaN", " 1", "1/2", "2147483648.0", "-2147483648.1", strings.Repeat("0", 65)} {
		value, err := decodeRecordAverageDecimal(raw)
		if !errors.Is(err, ErrRecordAverageResult) || value != "" {
			t.Fatal(raw, value, err)
		}
	}
	for _, raw := range []string{"2147483647.0000", "-2147483648.0000", "-0.0000"} {
		if value, err := decodeRecordAverageDecimal([]byte(raw)); err != nil || value != RecordDecimal(raw) {
			t.Fatal(value, err)
		}
	}
	bound := math.Nextafter(math.MaxFloat64/4, 0)
	if !safeFloatAccumulation(2, bound) || safeFloatAccumulation(2, math.Nextafter(bound, math.Inf(1))) || safeFloatAccumulation(1<<52+1, float64(0)) || safeFloatAccumulation(0, float64(0)) || safeFloatAccumulation(2, nil) {
		t.Fatal("unsafe shared accumulation boundary")
	}
	tx, p, _ := scalarFixture(t)
	column := sumTestColumn(t, p, "amount")
	if _, err := tx.ExecContext(t.Context(), "UPDATE "+p.table+" SET "+column+"=?", math.MaxFloat64); err != nil {
		t.Fatal(err)
	}
	got, err := p.AverageRecords(t.Context(), tx, "amount", nil, queryLimits())
	if got != nil || !errors.Is(err, ErrRecordAverageResult) {
		t.Fatal(got, err)
	}
	singleton := filterLeaf("number", "eq", int64(0))
	got, err = p.AverageRecords(t.Context(), tx, "amount", &singleton, queryLimits())
	if err != nil || got != float64(math.MaxFloat64) {
		t.Fatal(got, err)
	}
}
func TestRecordAverageExecutionFailures(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	assertNullableAggregateErrors(t, tx, p.AverageRecords)
}
