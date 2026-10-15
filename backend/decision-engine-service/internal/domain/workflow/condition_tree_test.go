package workflow

import (
	"encoding/json"
	"testing"
)

func TestConditionTree(t *testing.T) {
	tree := ConditionTree{Kind: "all", Children: []ConditionTree{
		{Kind: "outcome_in", Outcomes: []string{"review", "decline"}},
		{Kind: "any", Children: []ConditionTree{{Kind: "payload", Field: "amount", Operator: "gte", Value: json.RawMessage(`100`)}, {Kind: "payload", Field: "flagged", Operator: "eq", Value: json.RawMessage(`true`)}}},
	}}
	if err := tree.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, outcome string
		fields        map[string]any
		want          bool
	}{
		{"number", "review", map[string]any{"amount": 100}, true},
		{"boolean branch", "decline", map[string]any{"flagged": true}, true},
		{"wrong outcome", "approve", map[string]any{"amount": 1000}, false},
		{"missing", "review", nil, false},
		{"no coercion", "review", map[string]any{"amount": "1000", "flagged": "true"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tree.Matches(tc.outcome, tc.fields); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		op     string
		value  string
		fields map[string]any
		want   bool
	}{
		{"neq", `1`, nil, false}, {"exists", ``, map[string]any{"x": nil}, true}, {"not_exists", ``, nil, true},
		{"eq", `null`, map[string]any{"x": nil}, true}, {"contains", `"risk"`, map[string]any{"x": "high risk"}, true},
		{"gt", `9007199254740992`, map[string]any{"x": json.Number("9007199254740993")}, true},
	} {
		n := ConditionTree{Kind: "payload", Field: "x", Operator: tc.op, Value: json.RawMessage(tc.value)}
		if err := n.Validate(); err != nil {
			t.Fatal(err)
		}
		if n.Matches("", tc.fields) != tc.want {
			t.Fatal(tc)
		}
	}
}

func TestConditionTreeRejectsInvalidAndUnboundedInput(t *testing.T) {
	invalid := []ConditionTree{
		{Kind: "all"}, {Kind: "outcome_in", Outcomes: []string{"unknown"}}, {Kind: "outcome_in", Outcomes: []string{"review", "review"}},
		{Kind: "payload", Field: "x", Operator: "gt", Value: json.RawMessage(`"10"`)},
		{Kind: "payload", Field: "x", Operator: "eq", Value: json.RawMessage(`{}`)},
		{Kind: "always", Children: []ConditionTree{{Kind: "never"}}},
	}
	deep := ConditionTree{Kind: "always"}
	for i := 0; i < 4; i++ {
		deep = ConditionTree{Kind: "all", Children: []ConditionTree{deep}}
	}
	invalid = append(invalid, deep)
	wide := ConditionTree{Kind: "all"}
	for i := 0; i < 64; i++ {
		wide.Children = append(wide.Children, ConditionTree{Kind: "always"})
	}
	invalid = append(invalid, wide)
	for _, n := range invalid {
		if n.Validate() == nil {
			t.Fatalf("accepted invalid tree: %+v", n)
		}
	}
}
