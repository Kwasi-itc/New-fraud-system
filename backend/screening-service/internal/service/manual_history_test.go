package service

import (
	"net/url"
	"testing"
)

func TestManualHistoryRejectInvalidFilters(t *testing.T) {
	for _, q := range []url.Values{{"cursor": {"not-a-cursor"}}, {"limit": {"0"}}, {"status": {"invalid"}}, {"entity_type": {"Vehicle"}}, {"from": {"2026-02-30"}}, {"from": {"2026-02-01"}, "to": {"2026-01-01"}}, {"tenant_id": {"other"}}} {
		if _, err := ParseManualHistoryFilter(q); err == nil {
			t.Fatalf("accepted %v", q)
		}
	}
}
func TestManualAdvancedFields(t *testing.T) {
	in := manualInput()
	in.Nationality = " GB "
	in.BirthDate = "1980-01-31"
	in.Address = " Address "
	in.Datasets = []string{"us_ofac_sdn", "us_ofac_sdn"}
	req, err := in.Request("actor")
	if err != nil || req.Queries[0].Nationality != "gb" || req.Queries[0].BirthDate != "1980-01-31" {
		t.Fatal(req, err)
	}
	for _, change := range []func(*ManualSearchInput){func(v *ManualSearchInput) { v.BirthDate = "2026-02-30" }, func(v *ManualSearchInput) { v.BirthDate = "2999" }, func(v *ManualSearchInput) { v.Nationality = "ZZ" }, func(v *ManualSearchInput) { v.EntityType = "Organization" }, func(v *ManualSearchInput) { v.Datasets = []string{"../../bad"} }, func(v *ManualSearchInput) { v.Country = "us" }} {
		bad := in
		change(&bad)
		if _, err := bad.Request("actor"); err == nil {
			t.Fatal("invalid advanced field accepted", bad)
		}
	}
	org := manualInput()
	org.EntityType = "Organization"
	org.Country = "GH"
	org.RegistrationNumber = " 123 "
	req, err = org.Request("actor")
	if err != nil || req.Queries[0].Country != "gh" || req.Queries[0].RegistrationNumber != "123" {
		t.Fatal(req, err)
	}
}
