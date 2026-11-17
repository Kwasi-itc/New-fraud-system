package screening

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestInternalAuthoringRoundTripPreservesProviderProfileAndRefs(t *testing.T) {
	threshold := 85
	now := time.Now().UTC()
	r := AuthoringRule{ID: uuid.NewString(), Provider: "internal-watchlists", Profile: "name-fuzzy-v1", DatasetSelection: []DatasetRef{{Scope: "tenant", ID: "controlled-private"}, {Scope: "global", ID: "controlled-global"}}, Threshold: &threshold, Name: "Company screening", EntityType: "Organization", NameField: "company_name", RegistrationField: "registration", CountryField: "country", Outcomes: []string{"approve"}, InboxID: uuid.NewString()}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	config := Config{ID: r.ID, Name: r.Name, Provider: r.Provider, ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID, UpdatedAt: now}
	decoded, ok := DecodeAuthoring(config)
	if !ok || decoded.Provider != r.Provider || decoded.Profile != r.Profile || len(decoded.DatasetSelection) != 2 || decoded.RegistrationField != "registration" {
		t.Fatal(decoded, ok)
	}
	var raw map[string]any
	json.Unmarshal(config.ConfigJSON, &raw)
	if raw["authoring_version"] != float64(2) {
		t.Fatal(raw)
	}
	for _, mutate := range []func(*AuthoringRule){func(r *AuthoringRule) { r.Provider = "unknown" }, func(r *AuthoringRule) { r.Profile = "unversioned-fuzzy" }, func(r *AuthoringRule) { r.DatasetSelection = append(r.DatasetSelection, r.DatasetSelection[0]) }, func(r *AuthoringRule) { r.UseNER = true }, func(r *AuthoringRule) { r.Profile = "exact-name-v1" }} {
		bad := r
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsupported configuration accepted", bad)
		}
	}
	r.Profile = "identifier-evidence-v1"
	r.IdentifierFields = []IdentifierMapping{{Type: "registration", ValueField: "company_id", CountryField: "country"}}
	if e := r.Validate(); e != nil {
		t.Fatal(e)
	}
	config.ConfigJSON = r.ConfigJSON()
	roundtrip, ok := DecodeAuthoring(config)
	if !ok || len(roundtrip.IdentifierFields) != 1 || roundtrip.IdentifierFields[0].ValueField != "company_id" {
		t.Fatal("identifier mapping lost", roundtrip, ok)
	}
}
