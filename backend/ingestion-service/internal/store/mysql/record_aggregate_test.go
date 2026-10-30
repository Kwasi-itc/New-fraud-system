package mysql

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

// Shared assertions run separately for each public aggregate, not only a builder.
func assertAggregateCompilation(t *testing.T, p *RecordProjection, op string, rejected []string) {
	t.Helper()
	tree := filterGroup("and", filterLeaf("number", "in", []any{int64(0), nil}), filterLeaf("flag", "eq", true))
	q, args, field, err := p.compileAggregate(" number ", op, &tree, queryLimits())
	if err != nil {
		t.Fatal(err)
	}
	if field.DataType != "int" || !strings.HasPrefix(q, "SELECT ") || strings.Contains(q, " LIMIT ") || !reflect.DeepEqual(args, []any{int64(0), nil, true}) {
		t.Fatal(q, args, field)
	}
	exact := RecordQueryLimits{Parameters: 3, ParameterBytes: 10, SQLBytes: len(q)}
	if _, _, _, err = p.compileAggregate("number", op, &tree, exact); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*RecordQueryLimits){func(l *RecordQueryLimits) { l.Parameters-- }, func(l *RecordQueryLimits) { l.ParameterBytes-- }, func(l *RecordQueryLimits) { l.SQLBytes-- }} {
		limits := exact
		alter(&limits)
		q, a, f, err := p.compileAggregate("number", op, &tree, limits)
		if !errors.Is(err, ErrRecordQueryLimit) || q != "" || a != nil || f.Name != "" {
			t.Fatal(q, a, f, err)
		}
	}
	for _, name := range append([]string{"", "missing", "Number", "number`) FROM secrets --"}, rejected...) {
		q, a, f, err := p.compileAggregate(name, op, nil, queryLimits())
		if !errors.Is(err, ErrRecordAggregate) || q != "" || a != nil || f.Name != "" {
			t.Fatal(name, q, a, f, err)
		}
	}
	args[0] = int64(999)
	_, again, _, err := p.compileAggregate("number", op, &tree, exact)
	if err != nil || !reflect.DeepEqual(again, []any{int64(0), nil, true}) {
		t.Fatal(again, err)
	}
	invalid := filterLeaf("number", "eq", "0")
	if q, a, _, err := p.compileAggregate("number", op, &invalid, queryLimits()); !errors.Is(err, ErrRecordFilter) || q != "" || a != nil {
		t.Fatal(q, a, err)
	}
	var absent *RecordProjection
	if _, _, _, err := absent.compileAggregate("number", op, nil, queryLimits()); !errors.Is(err, ErrRecordAggregate) {
		t.Fatal(err)
	}
	for _, badOp := range []string{"SUM", "MIN) FROM secrets --", "avg"} {
		if _, _, _, err := p.compileAggregate("number", badOp, nil, queryLimits()); !errors.Is(err, ErrRecordAggregate) {
			t.Fatal(err)
		}
	}
	// Do not mutate the real projection: prove local invalid mappings fail closed.
	for _, change := range []func(*projectionField){func(f *projectionField) { f.definition.Archived = true }, func(f *projectionField) { f.width = 3 }, func(f *projectionField) { f.column = "" }} {
		copyP := *p
		copyP.fields = append([]projectionField(nil), p.fields...)
		for i := range copyP.fields {
			if copyP.fields[i].definition.Name == "number" {
				change(&copyP.fields[i])
			}
		}
		if _, _, _, err := copyP.compileAggregate("number", op, nil, queryLimits()); !errors.Is(err, ErrRecordAggregate) {
			t.Fatal(err)
		}
	}
}

type nullableAggregateCall func(context.Context, *sql.Tx, string, *ingestion.AggregateFilter, RecordQueryLimits) (any, error)

func assertNullableAggregateErrors(t *testing.T, tx *sql.Tx, call nullableAggregateCall) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := call(ctx, tx, "number", nil, queryLimits())
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	limits := queryLimits()
	limits.SQLBytes = 1
	got, err = call(t.Context(), tx, "number", nil, limits)
	if got != nil || !errors.Is(err, ErrRecordQueryLimit) {
		t.Fatal(got, err)
	}
	got, err = call(t.Context(), tx, "number", nil, queryLimits())
	if got != nil || !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(got, err)
	}
	got, err = call(t.Context(), nil, "number", nil, queryLimits())
	if got != nil || !errors.Is(err, ErrRecordAggregate) {
		t.Fatal(got, err)
	}
}
