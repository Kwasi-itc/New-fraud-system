package service

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"strings"
	"testing"
)

func TestScreeningAddressMapping(t *testing.T) {
	for _, entity := range []string{"Person", "Organization"} {
		rule := screening.AuthoringRule{EntityType: entity, NameField: "name", AddressField: "address"}
		cfg := screening.Config{Provider: "opensanctions", ConfigJSON: rule.ConfigJSON()}
		for _, tc := range []struct {
			value  any
			want   string
			failed bool
		}{
			{nil, "", false}, {"   ", "", false}, {"  Synthetic Street  ", "Synthetic Street", false}, {42, "", true}, {strings.Repeat("x", 301), "", true},
		} {
			raw, err := buildScreeningDispatchRequest("execution", cfg, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic", "address": tc.value})
			if (err != nil) != tc.failed {
				t.Fatal(err)
			}
			if err == nil {
				var req screeningDispatchRequest
				if json.Unmarshal(raw, &req) != nil || req.Queries[0].Address != tc.want {
					t.Fatal(string(raw))
				}
			}
		}
	}
}
func TestAuthoringAddressFailsClosedWithoutCapability(t *testing.T) {
	for _, reader := range []func(context.Context) (screening.AuthoringCapabilities, error){nil, func(context.Context) (screening.AuthoringCapabilities, error) {
		return screening.AuthoringCapabilities{}, nil
	}} {
		s := ScreeningAuthoringService{Capabilities: reader}
		if _, err := s.Save(context.Background(), "t", "s", workflow.AuthoringActor{}, screening.AuthoringRule{AddressField: "address"}); err == nil {
			t.Fatal("unsupported field accepted")
		}
	}
}
