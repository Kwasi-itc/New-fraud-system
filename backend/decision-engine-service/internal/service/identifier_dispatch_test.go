package service

import (
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/google/uuid"
	"testing"
)

func TestIdentifierMappingPreservesTextAndRejectsNumericCoercion(t *testing.T) {
	r := screening.AuthoringRule{ID: uuid.NewString(), Provider: "internal-watchlists", Profile: "identifier-evidence-v1", DatasetSelection: []screening.DatasetRef{{Scope: "tenant", ID: "private-fixture"}}, Name: "Fictional company check", EntityType: "Organization", NameField: "name", Outcomes: []string{"approve"}, InboxID: uuid.NewString(), IdentifierFields: []screening.IdentifierMapping{{Type: "registration", ValueField: "number", CountryField: "country"}}}
	if e := r.Validate(); e != nil {
		t.Fatal(e)
	}
	cfg := screening.Config{ID: r.ID, Name: r.Name, Provider: r.Provider, ConfigJSON: r.ConfigJSON(), ReviewInboxID: &r.InboxID, AllowedOutcomes: r.Outcomes}
	raw, e := buildScreeningDispatchRequest(uuid.NewString(), cfg, screening.Execution{}, "company", "fixture", map[string]any{"name": "Fictional Company", "number": "001-Ab/09", "country": "GH"})
	if e != nil {
		t.Fatal(e)
	}
	var out screeningDispatchRequest
	if e = json.Unmarshal(raw, &out); e != nil || len(out.Queries) != 1 || len(out.Queries[0].Identifiers) != 1 || out.Queries[0].Identifiers[0].Value != "001-Ab/09" || out.Queries[0].Identifiers[0].Country != "gh" {
		t.Fatal(out, e)
	}
	for _, bad := range []any{123.0, true, []string{"001"}, map[string]any{"value": "001"}} {
		if _, e = buildScreeningDispatchRequest(uuid.NewString(), cfg, screening.Execution{}, "company", "fixture", map[string]any{"name": "Fictional Company", "number": bad, "country": "GH"}); e == nil {
			t.Fatal("non-text identifier coerced", bad)
		}
	}
}
