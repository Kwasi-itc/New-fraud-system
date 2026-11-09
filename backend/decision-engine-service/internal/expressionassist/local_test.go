package expressionassist

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"strings"
	"testing"
)

func TestCompiledExpressionsEvaluateWithActualEngine(t *testing.T) {
	for _, item := range []struct {
		expression string
		want       bool
	}{
		{`amount >= 100 and currency == "USD"`, true},
		{`amount > 100 or currency == "EUR"`, false},
		{`true or false and false`, true},
		{`(true or false) and false`, false},
		{`not (amount < 100) and currency != "EUR"`, true},
		{`amount > -1.5e2`, true},
		{`"東京" == "東京"`, true},
	} {
		t.Run(item.expression, func(t *testing.T) {
			node, err := Compile(item.expression)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ast_eval.EvaluateNode(context.Background(), node, ast_eval.Runtime{Fields: map[string]any{"amount": 100.0, "currency": "USD"}})
			if err != nil || got != item.want {
				t.Fatal(got, err)
			}
			if description, err := Describe(node); err != nil || description == "" {
				t.Fatal(description, err)
			}
		})
	}
}
func TestCompilerRejectsMalformedAndUnboundedExpressions(t *testing.T) {
	for _, text := range []string{"", `amount = 1`, `amount === 1`, `amount > 1 garbage`, `a == 1; b == 2`, `true // ignored`, `1e9999 > 0`, strings.Repeat("(", 50) + "true" + strings.Repeat(")", 50), strings.Repeat("true or ", 300) + "true", `"unterminated`, `-currency == "USD"`} {
		if _, err := Compile(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
