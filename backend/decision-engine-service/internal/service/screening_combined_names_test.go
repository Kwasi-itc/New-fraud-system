package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
)

func TestCombinedScreeningNamesPreprocessOneQuery(t *testing.T) {
	rule := screening.AuthoringRule{EntityType: "Person", NameField: "first", AdditionalNameFields: []string{"middle", "last"}, RemoveDigits: true, MinimumNameLength: 5}
	for _, tc := range []struct {
		name    string
		fields  map[string]any
		want    string
		invalid bool
	}{
		{"ordered", map[string]any{"first": "  Alex12 ", "middle": " Q ", "last": " Example "}, "Alex Q Example", false},
		{"blank parts", map[string]any{"first": " ", "last": " Example "}, "Example", false},
		{"empty", map[string]any{"first": " ", "last": nil}, "", true},
		{"wrong type", map[string]any{"first": "Alex", "last": 123}, "", true},
		{"bounded combined name", map[string]any{"first": strings.Repeat("x", 195), "last": "Example"}, "", true},
		{"preprocessing skip", map[string]any{"first": "12", "last": "AB"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: rule.ConfigJSON()}, screening.Execution{}, "payment", "1", tc.fields)
			if tc.invalid {
				if !errors.Is(err, screening.ErrInvalidQuery) {
					t.Fatalf("expected invalid query, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if raw != nil {
					t.Fatal("preprocessing must skip the execution")
				}
				return
			}
			var request screeningDispatchRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Fatal(err)
			}
			if len(request.Queries) != 1 || request.Queries[0].Name != tc.want || request.Queries[0].Type != "Person" {
				t.Fatalf("unexpected query: %s", raw)
			}
		})
	}
}
