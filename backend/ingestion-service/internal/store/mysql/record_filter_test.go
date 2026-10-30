package mysql

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func filterLeaf(field, op string, value any) ingestion.AggregateFilter {
	return ingestion.AggregateFilter{Kind: "predicate", Field: field, Op: op, Value: value}
}
func filterGroup(op string, children ...ingestion.AggregateFilter) ingestion.AggregateFilter {
	return ingestion.AggregateFilter{Kind: "group", Operator: op, Children: children}
}
func TestRecordFilterTruthTables(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, tc := range []struct {
		tree ingestion.AggregateFilter
		want string
	}{
		{filterGroup("and", filterLeaf("number", "gte", int64(0)), filterLeaf("flag", "eq", true)), "b,c"},
		{filterGroup("or", filterLeaf("number", "lt", int64(0)), filterLeaf("number", "is_null", nil)), "a,n"},
		{filterGroup("not", filterLeaf("number", "eq", int64(0))), "a,c"},
		{filterGroup("not", filterLeaf("number", "in", []any{int64(0), nil})), ""},
		{filterGroup("and", filterGroup("or", filterLeaf("number", "in", []any{int64(-1), int64(1)}), filterLeaf("number", "is_null", nil)), filterGroup("not", filterLeaf("flag", "eq", true))), "a"},
	} {
		compiled, err := p.CompileFilter(&tc.tree)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(predicateMatches(t, tx, p, compiled), ","); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
func TestRecordFilterEmptySemantics(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, tree := range []*ingestion.AggregateFilter{nil, {}, ptrFilter(filterGroup("or")), ptrFilter(filterGroup("not", filterGroup("or"))), ptrFilter(filterGroup("and", filterGroup("or"), filterGroup("and")))} {
		compiled, err := p.CompileFilter(tree)
		clause, args := compiled.SQL()
		if err != nil || clause != "" || len(args) != 0 {
			t.Fatal(tree, clause, args, err)
		}
	}
	tree := filterGroup("or", filterGroup("and"), filterLeaf("number", "eq", int64(0)), filterGroup("not", filterGroup("or")))
	compiled, err := p.CompileFilter(&tree)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(predicateMatches(t, tx, p, compiled), ","); got != "b" {
		t.Fatal(got)
	}
	// Omitted kind/operator defaults to AND, matching the source builder.
	tree = ingestion.AggregateFilter{Children: []ingestion.AggregateFilter{filterLeaf("number", "eq", int64(0))}}
	compiled, err = p.CompileFilter(&tree)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(predicateMatches(t, tx, p, compiled), ","); got != "b" {
		t.Fatal(got)
	}
}
func ptrFilter(f ingestion.AggregateFilter) *ingestion.AggregateFilter { return &f }
func TestRecordFilterParameterOrderAndCopies(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	list := []any{int64(1), nil, int64(-1)}
	tree := filterGroup(" AnD ", filterLeaf("number", "gte", int64(-1)), filterGroup(" OR ", filterLeaf("number", " IN ", list), filterGroup(" NOT ", filterLeaf("flag", "eq", false))))
	compiled, err := p.CompileFilter(&tree)
	if err != nil {
		t.Fatal(err)
	}
	clause, args := compiled.SQL()
	want := []any{int64(-1), int64(1), nil, int64(-1), false}
	if !reflect.DeepEqual(args, want) || strings.Count(clause, "?") != 5 || !strings.Contains(clause, " AND ") || !strings.Contains(clause, " OR ") || !strings.Contains(clause, "NOT (") {
		t.Fatal(clause, args)
	}
	args[0] = int64(999)
	list[0] = int64(999)
	tree.Children = nil
	_, again := compiled.SQL()
	if !reflect.DeepEqual(again, want) {
		t.Fatal(again)
	}
	if got := strings.Join(predicateMatches(t, tx, p, compiled), ","); got != "a,b,c" {
		t.Fatal(got)
	}
}
func TestRecordFilterTraversalBounds(t *testing.T) {
	_, p, _ := scalarFixture(t)
	leaf := filterLeaf("number", "eq", int64(0))
	depth5 := leaf
	for i := 0; i < 4; i++ {
		depth5 = filterGroup("not", depth5)
	}
	if _, err := p.CompileFilter(&depth5); err != nil {
		t.Fatal(err)
	}
	// Weighted count is exactly 1 + 2 + 2 + 15*3 = 50.
	children := make([]ingestion.AggregateFilter, 15)
	for i := range children {
		children[i] = leaf
	}
	exact := filterGroup("and", leaf, filterGroup("or", children...))
	if _, err := p.CompileFilter(&exact); err != nil {
		t.Fatal("weighted 50", err)
	}
	flat := make([]ingestion.AggregateFilter, 20)
	for i := range flat {
		flat[i] = leaf
	}
	if _, err := p.CompileFilter(ptrFilter(filterGroup("and", flat...))); err != nil {
		t.Fatal("20 children", err)
	}
	for _, tree := range []ingestion.AggregateFilter{
		filterGroup("not", depth5),
		filterGroup("and", append(flat, leaf)...),
		filterGroup("and", leaf, filterGroup("or", append(children, leaf)...)),
		// Existing validator counts this 22-node tree as 63; retain its rejection.
		filterGroup("and", filterGroup("and", flat...)),
	} {
		compiled, err := p.CompileFilter(&tree)
		if !errors.Is(err, ErrRecordFilter) || compiled.clause != "" || len(compiled.args) != 0 {
			t.Fatal(compiled, err)
		}
	}
	// Caller-owned cyclic slices cannot cause unbounded validation recursion.
	cycle := filterGroup("and")
	cycle.Children = make([]ingestion.AggregateFilter, 1)
	cycle.Children[0] = cycle
	if _, err := p.CompileFilter(&cycle); !errors.Is(err, ErrRecordFilter) {
		t.Fatal(err)
	}
}
func TestRecordFilterFailureIsAtomic(t *testing.T) {
	_, p, _ := scalarFixture(t)
	good := filterLeaf("number", "eq", int64(0))
	hidden := good
	hidden.Children = []ingestion.AggregateFilter{good}
	for _, bad := range []ingestion.AggregateFilter{
		filterGroup("not"), filterGroup("not", good, good), filterGroup("xor", good), {Kind: "unknown"}, hidden,
		filterLeaf("number", "in", []any{}), filterLeaf("missing", "eq", nil), filterLeaf("number", "eq", "0"), filterLeaf("object_id", "starts_with", true),
	} {
		tree := filterGroup("and", good, bad)
		compiled, err := p.CompileFilter(&tree)
		if !errors.Is(err, ErrRecordFilter) || compiled.clause != "" || len(compiled.args) != 0 {
			t.Fatal(bad, compiled, err)
		}
	}
	var absent *RecordProjection
	if _, err := absent.CompileFilter(nil); !errors.Is(err, ErrRecordFilter) {
		t.Fatal(err)
	}
	tree := filterGroup("and", good, filterLeaf("number", "in", []any{}))
	_, err := p.CompileFilter(&tree)
	if !errors.Is(err, ErrRecordMembership) {
		t.Fatal("leaf cause lost", err)
	}
}
