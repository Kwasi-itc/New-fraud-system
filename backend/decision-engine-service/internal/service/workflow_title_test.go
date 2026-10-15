package service

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"testing"
)

func TestWorkflowCaseTitleSnapshot(t *testing.T) {
	raw := json.RawMessage(`{"name":"Case %object_id%: %outcome% (%score%)","title_mode":"template","inbox_id":"inbox"}`)
	for _, kind := range []workflow.ActionType{workflow.ActionTypeCreateCase, workflow.ActionTypeAddToCaseIfPossible} {
		got, err := workflowActionConfig(kind, raw, decision.Decision{ObjectID: "TX-42", Outcome: decision.OutcomeReview, Score: 75})
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err = json.Unmarshal(got, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg["name"] != "Case TX-42: review (75)" || cfg["inbox_id"] != "inbox" || cfg["title_mode"] != nil {
			t.Fatal(cfg)
		}
		if string(raw) != `{"name":"Case %object_id%: %outcome% (%score%)","title_mode":"template","inbox_id":"inbox"}` {
			t.Fatal("source configuration mutated")
		}
	}
	literal := json.RawMessage(`{"name":"Case %object_id%"}`)
	got, err := workflowActionConfig(workflow.ActionTypeCreateCase, literal, decision.Decision{ObjectID: "TX-42"})
	if err != nil || string(got) != string(literal) {
		t.Fatal("old literal title changed", string(got), err)
	}
}
