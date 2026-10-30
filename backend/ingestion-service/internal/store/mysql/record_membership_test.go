package mysql

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func TestMembershipMappingAndOwnership(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	input := []any{int64(1), nil, int64(-1), int64(1)}
	pred, err := p.MembershipPredicate(" number ", input)
	if err != nil {
		t.Fatal(err)
	}
	clause, args := pred.SQL()
	if strings.Contains(clause, "number") || !strings.HasSuffix(clause, " IN (?,?,?,?)") || !reflect.DeepEqual(args, input) {
		t.Fatal(clause, args)
	}
	input[0] = int64(99)
	args[2] = int64(99)
	_, again := pred.SQL()
	if !reflect.DeepEqual(again, []any{int64(1), nil, int64(-1), int64(1)}) {
		t.Fatal(again)
	}
	if got := strings.Join(predicateMatches(t, tx, p, pred), ","); got != "a,c" {
		t.Fatal(got)
	}
}
func TestMembershipTypedTruthTables(t *testing.T) {
	tx, p, at := scalarFixture(t)
	for _, tc := range []struct {
		field  string
		values []any
		want   string
	}{
		{"number", []any{int64(-1), int64(1)}, "a,c"},
		{"number", []any{int64(math.MinInt32), int64(math.MaxInt32)}, ""},
		{"amount", []any{float64(-0.5), float64(0.5)}, "a,c"},
		{"amount", []any{math.MaxFloat64}, ""},
		{"flag", []any{false}, "a"}, {"flag", []any{true, false}, "a,b,c"},
		{"updated_at", []any{at.In(time.FixedZone("offset", 3600)), at.Add(time.Microsecond)}, "b,c"},
	} {
		pred, err := p.MembershipPredicate(tc.field, tc.values)
		if err != nil {
			t.Fatal(tc, err)
		}
		if got := strings.Join(predicateMatches(t, tx, p, pred), ","); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
func TestMembershipNullLogic(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, values := range [][]any{{nil}, {nil, nil}, {int64(0), nil}} {
		pred, err := p.MembershipPredicate("number", values)
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if len(values) == 2 && values[0] != nil {
			want = "b"
		}
		if got := strings.Join(predicateMatches(t, tx, p, pred), ","); got != want {
			t.Fatal(values, got)
		}
		clause, args := pred.SQL()
		_, table := p.SQL()
		// Independently observe UNKNOWN rather than inferring it from WHERE alone.
		var unknown, negated int
		if err := tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table+" WHERE ("+clause+") IS NULL", args...).Scan(&unknown); err != nil {
			t.Fatal(err)
		}
		expected := 4
		if want == "b" {
			expected = 3
		}
		if unknown != expected {
			t.Fatal(values, unknown)
		}
		if err := tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table+" WHERE NOT ("+clause+")", args...).Scan(&negated); err != nil {
			t.Fatal(err)
		}
		if negated != 0 {
			t.Fatal("NOT converted unknown to true", negated)
		}
	}
	pred, err := p.MembershipPredicate("updated_at", []any{nil})
	if err != nil {
		t.Fatal(err)
	}
	if got := predicateMatches(t, tx, p, pred); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestMembershipListBounds(t *testing.T) {
	tx, p, _ := scalarFixture(t)
	for _, n := range []int{1, ingestion.MaxAggregateInListItems} {
		values := make([]any, n)
		for i := range values {
			values[i] = int64(0)
		}
		pred, err := p.MembershipPredicate("number", values)
		if err != nil {
			t.Fatal(n, err)
		}
		clause, args := pred.SQL()
		if len(args) != n || strings.Count(clause, "?") != n {
			t.Fatal(clause, args)
		}
		if got := strings.Join(predicateMatches(t, tx, p, pred), ","); got != "b" {
			t.Fatal(got)
		}
	}
	for _, value := range []any{nil, []any(nil), []any{}, make([]any, 101), []int64{0}, []string{"0"}, int64(0)} {
		pred, err := p.MembershipPredicate("number", value)
		if !errors.Is(err, ErrRecordMembership) || pred.clause != "" || len(pred.args) != 0 {
			t.Fatal(pred, err)
		}
	}
	var absent *RecordProjection
	if _, err := absent.MembershipPredicate("number", []any{nil}); !errors.Is(err, ErrRecordMembership) {
		t.Fatal(err)
	}
}
func TestMembershipRejectsInvalidMembersAndFields(t *testing.T) {
	_, p, at := scalarFixture(t)
	for _, tc := range []struct {
		field string
		value any
	}{
		{"number", "0"}, {"number", float64(0)}, {"number", int64(math.MaxInt32) + 1},
		{"amount", math.NaN()}, {"amount", math.Inf(-1)}, {"amount", int64(0)},
		{"flag", int64(1)}, {"updated_at", at.Add(time.Nanosecond)}, {"updated_at", at.Format(time.RFC3339)},
		{"object_id", int64(1)}, {"ip", "not-normalized"}, {"missing", nil}, {"Number", nil}, {"number` OR 1=1 --", nil},
	} {
		pred, err := p.MembershipPredicate(tc.field, []any{nil, tc.value})
		if !errors.Is(err, ErrRecordMembership) || pred.clause != "" || len(pred.args) != 0 {
			t.Fatal(tc, pred, err)
		}
	}
	values := make([]any, 100)
	for i := range values {
		values[i] = int64(0)
	}
	values[99] = "invalid final member"
	pred, err := p.MembershipPredicate("number", values)
	if !errors.Is(err, ErrRecordMembership) || pred.clause != "" || len(pred.args) != 0 {
		t.Fatal(pred, err)
	}
}
