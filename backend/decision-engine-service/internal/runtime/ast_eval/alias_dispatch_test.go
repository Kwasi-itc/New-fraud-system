package ast_eval

import (
	"context"
	"reflect"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
)

// Dispatch must retain the normalization used to select the operation. Test
// plain and evidence evaluation because both are used by decision services.
func TestNormalizedOperationDispatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []any
		want any
	}{
		{" + ", []any{2.0, 3.0}, 5.0},
		{" - ", []any{2.0, 3.0}, -1.0},
		{" * ", []any{2.0, 3.0}, 6.0},
		{" / ", []any{3.0, 2.0}, 1.5},
		{" StringStartsWith ", []any{"prefix-suffix", "prefix"}, true},
		{" StringStartsWith ", []any{"prefix-suffix", "suffix"}, false},
		{" StringEndsWith ", []any{"prefix-suffix", "suffix"}, true},
		{" LOWER ", []any{"ÉCOLE"}, "école"},
		{" UPPER ", []any{"école"}, "ÉCOLE"},
		{" SUM ", []any{[]any{1.0, 2.0}}, 3.0},
		{" AVG ", []any{[]any{1.0, 2.0}}, 1.5},
		{" MIN ", []any{[]any{1.0, 2.0}}, 1.0},
		{" MAX ", []any{[]any{1.0, 2.0}}, 2.0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := domainast.Node{Function: tc.name}
			for _, arg := range tc.args {
				node.Children = append(node.Children, domainast.Node{Constant: arg})
			}
			got, err := EvaluateNode(context.Background(), node, Runtime{})
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("plain: got %v, %v; want %v", got, err, tc.want)
			}
			evidence, err := evaluateNodeWithEvidence(context.Background(), node, Runtime{})
			if err != nil || !reflect.DeepEqual(evidence.ReturnValue, tc.want) {
				t.Fatalf("evidence: got %v, %v; want %v", evidence.ReturnValue, err, tc.want)
			}
			if evidence.Function != tc.name || len(evidence.Children) != len(tc.args) {
				t.Fatal("operation lost its source evidence")
			}
		})
	}
}
