package sourcedata

import (
	"testing"
)

func TestControlledNormalizationAndSelection(t *testing.T) {
	if !NormalizerSupported() {
		t.Fatal("Unicode tables changed; create a new normalizer/projection revision before release")
	}
	for _, name := range []string{" Ama   Fixture Alias ", "AMA FIXTURE ALIAS", "Ａｍａ Fixture Alias"} {
		if Normalize(name) != "ama fixture alias" {
			t.Fatalf("normalization: %q", Normalize(name))
		}
	}
	for _, raw := range []string{`{}`, `{"version":2,"profile":"unknown","dataset_selection":[{"scope":"global","id":"fixture"}]}`, `{"version":2,"profile":"exact-name-v1","dataset_selection":[{"scope":"tenant","id":"fixture"}],"owner_tenant_id":"forged"}`, `{"version":2,"profile":"exact-name-v1","dataset_selection":[{"scope":"tenant","id":"fixture"},{"scope":"tenant","id":"fixture"}]}`, `{"version":2,"profile":"exact-name-v1","dataset_selection":[{"scope":"global","id":"fixture"}]} {}`} {
		if _, err := DecodeSelection([]byte(raw)); err == nil {
			t.Fatalf("invalid selection accepted: %s", raw)
		}
	}
	if _, err := DecodeSelection([]byte(`{"version":2,"profile":"exact-name-v1","threshold":100,"dataset_selection":[{"scope":"global","id":"fixture"}]}`)); err != nil {
		t.Fatal(err)
	}
}
