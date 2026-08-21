package eventstore

import "testing"

func TestConditionalAggregateExpression(t *testing.T) {
	tests := map[string]string{
		"count":          "countIf((((account_ref = 'acct-1'))) AND isNotNull(`amount`))",
		"count_distinct": "uniqExactIf(`amount`, ((account_ref = 'acct-1')))",
		"sum":            "coalesce(sumIf(`amount`, ((account_ref = 'acct-1'))), 0)",
		"avg":            "avgIf(`amount`, ((account_ref = 'acct-1')))",
	}
	for aggregate, want := range tests {
		got, err := conditionalAggregateExpression(aggregate, "`amount`", "((account_ref = 'acct-1'))")
		if err != nil {
			t.Fatalf("%s: %v", aggregate, err)
		}
		if got != want {
			t.Fatalf("%s expression = %q, want %q", aggregate, got, want)
		}
	}
}
