package postgres

import (
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
)

func TestRuleExecutionEvaluationRoundTrip(t *testing.T) {
	t.Parallel()

	original := &domainast.EvaluationNode{
		Function:    "gt",
		ReturnValue: true,
		Children: []domainast.EvaluationNode{
			{Function: "Payload", Constant: "amount", ReturnValue: 500.0},
			{Function: "constant", Constant: 300.0, ReturnValue: 300.0},
		},
	}

	raw, err := marshalRuleEvaluation(original)
	if err != nil {
		t.Fatalf("marshalRuleEvaluation() error = %v", err)
	}

	roundTrip, err := unmarshalRuleEvaluation(raw)
	if err != nil {
		t.Fatalf("unmarshalRuleEvaluation() error = %v", err)
	}
	if roundTrip == nil {
		t.Fatal("unmarshalRuleEvaluation() = nil, want evaluation")
	}
	if roundTrip.Function != "gt" {
		t.Fatalf("roundTrip.Function = %q, want gt", roundTrip.Function)
	}
	if roundTrip.ReturnValue != true {
		t.Fatalf("roundTrip.ReturnValue = %#v, want true", roundTrip.ReturnValue)
	}
	if len(roundTrip.Children) != 2 {
		t.Fatalf("len(roundTrip.Children) = %d, want 2", len(roundTrip.Children))
	}
	if got := roundTrip.Children[0].ReturnValue; got != 500.0 {
		t.Fatalf("left child return value = %#v, want 500.0", got)
	}
	if got := roundTrip.Children[1].ReturnValue; got != 300.0 {
		t.Fatalf("right child return value = %#v, want 300.0", got)
	}
}

func TestRuleExecutionEvaluationRoundTripNil(t *testing.T) {
	t.Parallel()

	raw, err := marshalRuleEvaluation(nil)
	if err != nil {
		t.Fatalf("marshalRuleEvaluation(nil) error = %v", err)
	}
	if raw != nil {
		t.Fatalf("marshalRuleEvaluation(nil) = %#v, want nil", raw)
	}

	roundTrip, err := unmarshalRuleEvaluation(nil)
	if err != nil {
		t.Fatalf("unmarshalRuleEvaluation(nil) error = %v", err)
	}
	if roundTrip != nil {
		t.Fatalf("unmarshalRuleEvaluation(nil) = %#v, want nil", roundTrip)
	}
}
