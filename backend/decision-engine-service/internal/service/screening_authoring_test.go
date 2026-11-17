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

func TestInternalAuthoredDispatchRejectsUnsupportedRuntimeProfile(t *testing.T) {
	r := screening.AuthoringRule{ID: uuid.NewString(), Provider: "internal-watchlists", Profile: "name-fuzzy-v1", DatasetSelection: []screening.DatasetRef{{Scope: "tenant", ID: "controlled-private"}}, Name: "Internal beneficiary", EntityType: "Person", NameField: "beneficiary_name", InboxID: uuid.NewString(), Outcomes: []string{"approve"}}
	cfg := screening.Config{ID: r.ID, Name: r.Name, Provider: r.Provider, ConfigJSON: r.ConfigJSON(), ReviewInboxID: &r.InboxID, AllowedOutcomes: r.Outcomes}
	raw, err := buildScreeningDispatchRequest(uuid.NewString(), cfg, screening.Execution{DecisionID: uuid.NewString(), ScenarioID: uuid.NewString()}, "transaction", "object", map[string]any{"beneficiary_name": "Ama Testpreson"})
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Provider string         `json:"provider"`
		Config   map[string]any `json:"provider_config"`
	}
	if json.Unmarshal(raw, &request) != nil || request.Provider != r.Provider || request.Config["profile"] != r.Profile {
		t.Fatal("selected configuration did not reach dispatch", string(raw))
	}
	var malformed map[string]any
	json.Unmarshal(cfg.ConfigJSON, &malformed)
	malformed["provider_config"].(map[string]any)["profile"] = "unversioned-fuzzy"
	cfg.ConfigJSON, _ = json.Marshal(malformed)
	if _, err = buildScreeningDispatchRequest(uuid.NewString(), cfg, screening.Execution{}, "transaction", "object", map[string]any{"beneficiary_name": "Ama Testperson"}); err == nil {
		t.Fatal("unsupported profile reached durable dispatch")
	}
}
