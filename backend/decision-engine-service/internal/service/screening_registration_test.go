package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"strings"
	"testing"
)

func TestScreeningRegistrationMapping(t *testing.T) {
	for _, tc := range []struct {
		value   any
		want    string
		invalid bool
	}{{nil, "", false}, {"  ", "", false}, {"  001-Ab/09  ", "001-Ab/09", false}, {strings.Repeat("?", 100), strings.Repeat("?", 100), false}, {strings.Repeat("x", 101), "", true}, {42, "", true}, {[]string{"001"}, "", true}} {
		r := screening.AuthoringRule{EntityType: "Organization", NameField: "name", RegistrationField: "registration"}
		raw, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: r.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic", "registration": tc.value})
		if (err != nil) != tc.invalid {
			t.Fatalf("registration validation: %v", err)
		}
		if tc.invalid && !errors.Is(err, screening.ErrInvalidQuery) {
			t.Fatal("untyped registration failure", err)
		}
		if err == nil {
			var req screeningDispatchRequest
			if json.Unmarshal(raw, &req) != nil || req.Queries[0].RegistrationNumber != tc.want {
				t.Fatal(string(raw))
			}
		}
	}
	r := screening.AuthoringRule{EntityType: "Person", NameField: "name", RegistrationField: "registration"}
	if _, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: r.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic", "registration": "001"}); err == nil {
		t.Fatal("Person registration accepted")
	}
	svc := ScreeningAuthoringService{Capabilities: func(context.Context) (screening.AuthoringCapabilities, error) {
		return screening.AuthoringCapabilities{CountrySupported: true}, nil
	}}
	if _, err := svc.Save(context.Background(), "t", "s", workflow.AuthoringActor{}, r); err != screening.ErrRegistrationUnavailable {
		t.Fatal("registration capability not enforced", err)
	}
}
