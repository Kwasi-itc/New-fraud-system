package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"testing"
)

func TestScenarioMatchingDispatch(t *testing.T) {
	threshold, limit := 0, 50
	rule := screening.AuthoringRule{EntityType: "Person", NameField: "name", Threshold: &threshold, ResultLimit: &limit, Datasets: []string{"synthetic"}}
	raw, err := buildScreeningDispatchRequest("e", screening.Config{Provider: "opensanctions", ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	var req screeningDispatchRequest
	json.Unmarshal(raw, &req)
	var config struct {
		Threshold *int     `json:"threshold"`
		Datasets  []string `json:"datasets"`
	}
	json.Unmarshal(req.ProviderConfig, &config)
	if req.LimitOverride == nil || *req.LimitOverride != 50 || config.Threshold == nil || *config.Threshold != 0 || len(config.Datasets) != 1 {
		t.Fatal(string(raw))
	}
	for _, rule := range []screening.AuthoringRule{{Threshold: &threshold}, {ResultLimit: &limit}, {Datasets: []string{"synthetic"}}} {
		_, err := (ScreeningAuthoringService{}).Save(context.Background(), "t", "s", workflow.AuthoringActor{}, rule)
		if !errors.Is(err, screening.ErrMatchingUnavailable) {
			t.Fatal(err)
		}
	}
}
