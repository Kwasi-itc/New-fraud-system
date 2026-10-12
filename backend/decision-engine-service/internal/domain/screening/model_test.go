package screening

import "testing"

func TestConfigReviewInboxValidation(t *testing.T) {
	c := Config{TenantID: "tenant", ScenarioID: "scenario", Name: "Review", Provider: "test", AllowedOutcomes: []string{"review"}}
	for _, id := range []string{"", "invalid", "00000000-0000-0000-0000-000000000000"} {
		c.ReviewInboxID = &id
		if c.Validate() == nil {
			t.Fatalf("accepted route %q", id)
		}
	}
	id := "11111111-1111-4111-8111-111111111111"
	c.ReviewInboxID = &id
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.ReviewInboxID = nil
	if err := c.Validate(); err != nil {
		t.Fatal("legacy missing route", err)
	}
}
