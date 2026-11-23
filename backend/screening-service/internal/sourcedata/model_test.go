package sourcedata

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestFullCatalogSelectionPreservesEveryReference(t *testing.T) {
	selection := Selection{Version: 2, Profile: FuzzyProfile, Threshold: 70}
	for i := 0; i < MaxSelectedDatasets; i++ {
		selection.Datasets = append(selection.Datasets, DatasetRef{Scope: "global", ID: fmt.Sprintf("catalog-%03d", i)})
	}
	raw, _ := json.Marshal(selection)
	decoded, e := DecodeSelection(raw)
	if e != nil || len(decoded.Datasets) != MaxSelectedDatasets {
		t.Fatal("full catalog rejected or truncated", len(decoded.Datasets), e)
	}
	selection.Datasets = append(selection.Datasets, DatasetRef{Scope: "global", ID: "one-beyond-catalog"})
	raw, _ = json.Marshal(selection)
	if _, e = DecodeSelection(raw); e == nil {
		t.Fatal("resource ceiling ignored")
	}
}

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
