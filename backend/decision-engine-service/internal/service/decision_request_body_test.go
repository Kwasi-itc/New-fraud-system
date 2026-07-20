package service

import (
	"encoding/json"
	"testing"
)

func TestDecisionEvaluationRequestJSONBody(t *testing.T) {
	t.Parallel()

	body, err := (DecisionEvaluationRequest{
		ObjectID:   "txn_123",
		ObjectType: "transactions",
		Fields: map[string]any{
			"amount":   12500,
			"currency": "USD",
		},
	}).JSONBody()
	if err != nil {
		t.Fatalf("JSONBody() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if decoded["object_id"] != "txn_123" {
		t.Fatalf("object_id = %v, want txn_123", decoded["object_id"])
	}
	if decoded["object_type"] != "transactions" {
		t.Fatalf("object_type = %v, want transactions", decoded["object_type"])
	}
	fields, ok := decoded["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields type = %T, want object", decoded["fields"])
	}
	if fields["currency"] != "USD" {
		t.Fatalf("currency = %v, want USD", fields["currency"])
	}
}
