package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func queryLimits() RecordQueryLimits {
	return RecordQueryLimits{Parameters: 5000, ParameterBytes: 1 << 20, SQLBytes: 1 << 20}
}
func queryResults() RecordResultLimits {
	return RecordResultLimits{FieldBytes: 128, TransferBytes: 1024, RowBytes: 4096, PageBytes: 16384, Rows: 10}
}
func TestRecordQueryLimitConfiguration(t *testing.T) {
	good := queryLimits()
	for _, alter := range []func(*RecordQueryLimits){
		func(l *RecordQueryLimits) { l.Parameters = 0 }, func(l *RecordQueryLimits) { l.Parameters = 60001 },
		func(l *RecordQueryLimits) { l.ParameterBytes = 0 }, func(l *RecordQueryLimits) { l.ParameterBytes = 16<<20 + 1 },
		func(l *RecordQueryLimits) { l.SQLBytes = 0 }, func(l *RecordQueryLimits) { l.SQLBytes = 1<<20 + 1 },
	} {
		l := good
		alter(&l)
		if !errors.Is(ValidateRecordQuery("SELECT 1", nil, l), ErrRecordQueryLimit) {
			t.Fatal(l)
		}
	}
	if err := ValidateRecordQuery("SELECT 1", nil, good); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", strings.Repeat("x", good.SQLBytes+1)} {
		if !errors.Is(ValidateRecordQuery(query, nil, good), ErrRecordQueryLimit) {
			t.Fatal("query accepted")
		}
	}
}
func TestRecordQueryValueAccounting(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	// 1 + 1 + 8 + 8 + 8 + 3 + 2 + 16 + 32 = 79 accounted bytes.
	args := []any{nil, true, int(1), int64(2), float64(3), "abc", []byte{0, 255}, mysqlstore.UUID(uuid.New()), now}
	limits := RecordQueryLimits{Parameters: len(args), ParameterBytes: 79, SQLBytes: 8}
	if err := ValidateRecordQuery("SELECT ?", args, limits); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RecordQueryLimits){func(l *RecordQueryLimits) { l.ParameterBytes-- }, func(l *RecordQueryLimits) { l.Parameters-- }, func(l *RecordQueryLimits) { l.SQLBytes-- }} {
		l := limits
		change(&l)
		if !errors.Is(ValidateRecordQuery("SELECT ?", args, l), ErrRecordQueryLimit) {
			t.Fatal(l)
		}
	}
	key := strings.Repeat("k", 32)
	if err := ValidateRecordQuery("SELECT ?", []any{key, key}, RecordQueryLimits{Parameters: 2, ParameterBytes: 63, SQLBytes: 8}); !errors.Is(err, ErrRecordQueryLimit) {
		t.Fatal("repeated key undercount", err)
	}
	for _, value := range []any{uint64(1), math.NaN(), math.Inf(1), now.Add(time.Nanosecond), []any{1}, map[string]any{"x": 1}, unexpectedQueryValuer{}} {
		if !errors.Is(ValidateRecordQuery("SELECT ?", []any{value}, queryLimits()), ErrRecordQueryLimit) {
			t.Fatal(value)
		}
	}
}

// Unknown values must be rejected without calling methods or string conversion.
type unexpectedQueryValuer struct{}

func (unexpectedQueryValuer) Value() (driver.Value, error) { panic("unexpected Valuer call") }
func (unexpectedQueryValuer) String() string               { panic("unexpected value formatting") }
func TestRecordQueryCompositionBudget(t *testing.T) {
	_, p, _ := scalarFixture(t)
	tree := filterGroup("and", filterLeaf("number", "in", []any{int64(0), nil}), filterLeaf("flag", "eq", true))
	query, args, _, err := p.compileSelection(&tree, queryResults(), queryLimits())
	if err != nil {
		t.Fatal(err)
	}
	want := []any{128, int64(0), nil, true, int64(10)}
	if !reflect.DeepEqual(args, want) || !strings.Contains(query, " WHERE ") || !strings.HasSuffix(query, " LIMIT ?") {
		t.Fatal(query, args)
	}
	exact := RecordQueryLimits{Parameters: 5, ParameterBytes: 26, SQLBytes: len(query)}
	if _, _, _, err := p.compileSelection(&tree, queryResults(), exact); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RecordQueryLimits){func(l *RecordQueryLimits) { l.Parameters-- }, func(l *RecordQueryLimits) { l.ParameterBytes-- }, func(l *RecordQueryLimits) { l.SQLBytes-- }} {
		l := exact
		change(&l)
		q, a, b, err := p.compileSelection(&tree, queryResults(), l)
		if !errors.Is(err, ErrRecordQueryLimit) || q != "" || a != nil || b != nil {
			t.Fatal(q, a, b, err)
		}
	}
	args[0] = 999
	_, again, _, err := p.compileSelection(&tree, queryResults(), exact)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatal(again, err)
	}
}
func TestRecordQuerySelection(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	tree := filterGroup("and", filterLeaf("number", "in", []any{int64(0), nil}), filterLeaf("flag", "eq", true))
	records, err := p.SelectRecords(t.Context(), tx, &tree, queryResults(), queryLimits())
	if err != nil || len(records) != 1 || records[0]["object_id"] != "b" {
		t.Fatal(records, err)
	}
	for _, filter := range []*ingestion.AggregateFilter{nil, ptrFilter(filterGroup("or"))} {
		results := queryResults()
		results.Rows = 1
		records, err = p.SelectRecords(t.Context(), tx, filter, results, queryLimits())
		if err != nil || len(records) != 1 {
			t.Fatal(records, err)
		}
		q, _, _, err := p.compileSelection(filter, results, queryLimits())
		if err != nil || strings.Contains(q, " WHERE ") {
			t.Fatal(q, err)
		}
	}
	tree = filterLeaf("number", "eq", int64(99))
	records, err = p.SelectRecords(t.Context(), tx, &tree, queryResults(), queryLimits())
	if err != nil || records == nil || len(records) != 0 {
		t.Fatal(records, err)
	}
	results := queryResults()
	results.PageBytes = results.RowBytes
	results.RowBytes = 450
	results.PageBytes = 450
	records, err = p.SelectRecords(t.Context(), tx, nil, results, queryLimits())
	if !errors.Is(err, ErrRecordResultLimit) || records != nil {
		t.Fatal(records, err)
	}
}
func TestRecordQueryRejectsBeforeExecution(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	limits := queryLimits()
	limits.Parameters = 1 // string guard + LIMIT already require two.
	records, err := p.SelectRecords(t.Context(), tx, nil, queryResults(), limits)
	if records != nil || !errors.Is(err, ErrRecordQueryLimit) {
		t.Fatal("budget must fail before closed transaction", records, err)
	}
	records, err = p.SelectRecords(t.Context(), tx, nil, queryResults(), queryLimits())
	if records != nil || !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(records, err)
	}
	liveTx, liveP, _ := scalarFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	records, err = liveP.SelectRecords(ctx, liveTx, nil, queryResults(), queryLimits())
	if records != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(records, err)
	}
	if _, err = liveP.SelectRecords(t.Context(), nil, nil, queryResults(), queryLimits()); !errors.Is(err, ErrRecordProjection) {
		t.Fatal(err)
	}
}
