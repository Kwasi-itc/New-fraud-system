package service

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExecutionEventDisabledIsExplicitButMissingEnabledDependenciesFail(t *testing.T) {
	s := DecisionService{}
	event, err := s.PrepareExecutionEvent("tenant", "scheduled_execution", "execution", "scheduled_execution.completed", map[string]any{"status": "completed"})
	if err != nil || event.event != nil {
		t.Fatal("disabled event", event, err)
	}
	if err := event.Persist(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	s.publishOutboxEvents = true
	if _, err := s.PrepareExecutionEvent("tenant", "scheduled_execution", "execution", "scheduled_execution.completed", nil); err == nil {
		t.Fatal("missing enabled dependencies ignored")
	}
}
func TestCloneExecutionRequestsDoesNotSerializePrivateAuthority(t *testing.T) {
	input := []DecisionEvaluationRequest{{ObjectID: " exact ", ObjectType: "wallets", Fields: map[string]any{"amount": 10}, prepared: &preparedObject{tenant: "not-caller-authority"}, storedSource: true}, {ObjectID: "stored", ObjectType: "wallets"}}
	copy, err := CloneExecutionRequests(input, ExecutionPreparationLimits{MaxItems: 10, MaxBytes: 1 << 20})
	if err != nil || copy[0].prepared != nil || copy[0].storedSource || copy[1].Fields != nil || copy[0].ObjectID != " exact " {
		t.Fatal(copy, err)
	}
	input[0].Fields["amount"] = 20
	if copy[0].Fields["amount"].(json.Number).String() != "10" {
		t.Fatal("caller alias retained")
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	input[0].Fields = cycle
	if _, err := CloneExecutionRequests(input, ExecutionPreparationLimits{MaxItems: 10, MaxBytes: 1 << 20}); err == nil {
		t.Fatal("unbounded cyclic input accepted")
	}
}
