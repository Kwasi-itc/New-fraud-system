package service

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/google/uuid"
	"testing"
)

func TestAuthoredConfigurationDispatchesMappedName(t *testing.T) {
	for _, entity := range []string{"Person", "Organization"} {
		r := screening.AuthoringRule{ID: uuid.NewString(), Name: "Beneficiary", EntityType: entity, NameField: "beneficiary_name", InboxID: uuid.NewString(), Outcomes: []string{"review"}}
		cfg := screening.Config{ID: r.ID, Provider: "opensanctions", ConfigJSON: r.ConfigJSON()}
		raw, err := buildScreeningDispatchRequest("execution", cfg, screening.Execution{DecisionID: "decision", ScenarioID: "scenario"}, "transaction", "object", map[string]any{"beneficiary_name": " Synthetic Entity "})
		if err != nil {
			t.Fatal(err)
		}
		var out screeningDispatchRequest
		if err = json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Queries) != 1 || out.Queries[0].Name != "Synthetic Entity" || out.Queries[0].Type != entity || out.Provider != "opensanctions" || out.ScreeningConfigID != r.ID {
			t.Fatal(string(raw))
		}
		if _, err = buildScreeningDispatchRequest("execution", cfg, screening.Execution{}, "transaction", "object", nil); err == nil {
			t.Fatal("missing mapped value accepted")
		}
	}
}
