package service

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"testing"
)

func TestScreeningBirthDateMapping(t *testing.T) {
	for _, tc := range []struct {
		value   any
		want    string
		invalid bool
	}{{nil, "", false}, {"  ", "", false}, {" 1980 ", "1980", false}, {"2000-02-29", "2000-02-29", false}, {"2001-02-29", "", true}, {"0000", "", true}, {"2999", "", true}, {"1990-01-01T00:00:00Z", "", true}, {1990, "", true}, {"1990-1-1", "", true}} {
		r := screening.AuthoringRule{EntityType: "Person", NameField: "name", BirthDateField: "dob"}
		cfg := screening.Config{Provider: "opensanctions", ConfigJSON: r.ConfigJSON()}
		raw, err := buildScreeningDispatchRequest("e", cfg, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic", "dob": tc.value})
		if (err != nil) != tc.invalid {
			t.Fatalf("%v: %v", tc.value, err)
		}
		if err == nil {
			var req screeningDispatchRequest
			if json.Unmarshal(raw, &req) != nil || req.Queries[0].BirthDate != tc.want {
				t.Fatal(string(raw))
			}
		}
	}
	r := screening.AuthoringRule{EntityType: "Organization", NameField: "name", BirthDateField: "dob"}
	if _, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: r.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic", "dob": "1980"}); err == nil {
		t.Fatal("organization birth date accepted")
	}
}
func TestBirthDateCapabilityIndependentOfAddress(t *testing.T) {
	s := ScreeningAuthoringService{Capabilities: func(context.Context) (screening.AuthoringCapabilities, error) {
		return screening.AuthoringCapabilities{AddressSupported: true}, nil
	}}
	if _, err := s.Save(context.Background(), "t", "s", workflow.AuthoringActor{}, screening.AuthoringRule{BirthDateField: "dob"}); err != screening.ErrBirthDateUnavailable {
		t.Fatal(err)
	}
}
