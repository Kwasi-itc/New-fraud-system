package service

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"testing"
)

func TestScreeningCountryMappings(t *testing.T) {
	for _, entity := range []string{"Person", "Organization"} {
		for _, tc := range []struct {
			value   any
			want    string
			invalid bool
		}{{nil, "", false}, {"  ", "", false}, {" GB ", "gb", false}, {"US", "us", false}, {"ZZ", "", true}, {"001", "", true}, {"USA", "", true}, {"United Kingdom", "", true}, {42, "", true}, {[]string{"gb"}, "", true}} {
			r := screening.AuthoringRule{EntityType: entity, NameField: "name"}
			if entity == "Person" {
				r.NationalityField = "code"
			} else {
				r.CountryField = "code"
			}
			raw, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: r.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic", "code": tc.value})
			if (err != nil) != tc.invalid {
				t.Fatalf("%s %v: %v", entity, tc.value, err)
			}
			if err == nil {
				var req screeningDispatchRequest
				if json.Unmarshal(raw, &req) != nil {
					t.Fatal(string(raw))
				}
				q := req.Queries[0]
				if entity == "Person" {
					if q.Nationality != tc.want || q.Country != "" {
						t.Fatal(q)
					}
				} else if q.Country != tc.want || q.Nationality != "" {
					t.Fatal(q)
				}
			}
		}
	}
	for _, r := range []screening.AuthoringRule{{EntityType: "Person", NameField: "name", CountryField: "code"}, {EntityType: "Organization", NameField: "name", NationalityField: "code"}} {
		if _, err := buildScreeningDispatchRequest("e", screening.Config{ConfigJSON: r.ConfigJSON()}, screening.Execution{}, "customer", "1", map[string]any{"name": "Synthetic", "code": "gb"}); err == nil {
			t.Fatal("cross-entity mapping accepted")
		}
	}
}
func TestCountryCapabilitiesFailClosed(t *testing.T) {
	svc := ScreeningAuthoringService{Capabilities: func(context.Context) (screening.AuthoringCapabilities, error) {
		return screening.AuthoringCapabilities{AddressSupported: true, BirthDateSupported: true}, nil
	}}
	for _, r := range []screening.AuthoringRule{{NationalityField: "code"}, {CountryField: "code"}} {
		if _, err := svc.Save(context.Background(), "t", "s", workflow.AuthoringActor{}, r); err == nil {
			t.Fatal("unsupported mapping accepted")
		}
	}
}
