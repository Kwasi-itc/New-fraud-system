package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"testing"
)

func TestScenarioNamePreprocessing(t *testing.T) {
	rule := screening.AuthoringRule{EntityType: "Person", NameField: "name", MinimumNameLength: 3, RemoveDigits: true}
	for _, tc := range []struct {
		name, want string
		skip       bool
	}{{"12Synthetic34", "Synthetic", false}, {"1AB2", "", true}, {"1234", "", true}, {"AB", "", true}, {"???", "???", false}, {"\u0661Synthetic\u0662", "Synthetic", false}} {
		raw, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": tc.name})
		if err != nil {
			t.Fatal(err)
		}
		if (raw == nil) != tc.skip {
			t.Fatal(tc, string(raw))
		}
		if raw != nil {
			var request screeningDispatchRequest
			json.Unmarshal(raw, &request)
			if request.Queries[0].Name != tc.want {
				t.Fatal(tc, string(raw))
			}
		}
	}
	if _, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": 123}); err == nil {
		t.Fatal("authored numeric name accepted")
	}
}

func TestScenarioIgnoreWords(t *testing.T) {
	rule := screening.AuthoringRule{EntityType: "Organization", NameField: "name", IgnoreListID: "list", RemoveDigits: true, MinimumNameLength: 3}
	for _, tc := range []struct{ name, want string }{{"12Synthetic LTD", "Synthetic"}, {"LTD", ""}, {"ltd. Synthetic", "ltd. Synthetic"}, {"LtdSynthetic", "LtdSynthetic"}} {
		raw, err := buildScreeningDispatchWithWords("e", screening.Config{ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": tc.name}, []string{"ltd"})
		if err != nil {
			t.Fatal(err)
		}
		if tc.want == "" {
			if raw != nil {
				t.Fatal(string(raw))
			}
			continue
		}
		var req screeningDispatchRequest
		json.Unmarshal(raw, &req)
		if req.Queries[0].Name != tc.want {
			t.Fatal(string(raw))
		}
	}
}

func TestScenarioNameRecognition(t *testing.T) {
	rule := screening.AuthoringRule{EntityType: "Person", NameField: "name", UseNER: true, RemoveDigits: true}
	calls := 0
	recognizer := func(ctx context.Context, name string) ([]screening.RecognizedName, error) {
		calls++
		return []screening.RecognizedName{{Type: "Person", Text: "Alice123"}, {Type: "Company", Text: "Acme"}, {Type: "Location", Text: "Elsewhere"}, {Type: "Person", Text: "Alice123"}}, nil
	}
	raw, err := buildScreeningDispatchPreprocessed(context.Background(), "e", screening.Config{ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Original input"}, nil, recognizer)
	var req screeningDispatchRequest
	json.Unmarshal(raw, &req)
	if err != nil || calls != 1 || len(req.Queries) != 3 || req.Queries[0].Name != "Original input" || req.Queries[1].Name != "Alice" || req.Queries[2].Type != "Organization" {
		t.Fatal(string(raw), err)
	}
	rule.IgnoreNERClassification = true
	raw, err = buildScreeningDispatchPreprocessed(context.Background(), "e", screening.Config{ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Original input"}, nil, recognizer)
	json.Unmarshal(raw, &req)
	if err != nil || len(req.Queries) != 4 || req.Queries[3].Type != "Thing" {
		t.Fatal(string(raw), err)
	}
	_, err = buildScreeningDispatchPreprocessed(context.Background(), "e", screening.Config{ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Original"}, nil, func(context.Context, string) ([]screening.RecognizedName, error) { return nil, errors.New("private") })
	if !errors.Is(err, screening.ErrRecognitionFailed) {
		t.Fatal(err)
	}
}
