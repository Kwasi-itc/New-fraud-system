package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
)

func TestAdaptDecisionDetailIncludesRequestBody(t *testing.T) {
	t.Parallel()

	input := decision.Decision{
		ID:                  "dec_1",
		TenantID:            "tenant_1",
		ScenarioID:          "scenario_1",
		ScenarioIterationID: "iteration_1",
		ObjectID:            "txn_123",
		ObjectType:          "transactions",
		RequestBody:         json.RawMessage(`{"object_id":"txn_123","object_type":"transactions","fields":{"amount":12500}}`),
		Outcome:             decision.OutcomeReview,
		Score:               65,
		Triggered:           true,
		CreatedAt:           time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC),
	}

	output := AdaptDecisionDetail(input)
	if string(output.RequestBody) != string(input.RequestBody) {
		t.Fatalf("request body = %s, want %s", output.RequestBody, input.RequestBody)
	}
	if output.Outcome != string(decision.OutcomeReview) {
		t.Fatalf("outcome = %s, want %s", output.Outcome, decision.OutcomeReview)
	}
}
