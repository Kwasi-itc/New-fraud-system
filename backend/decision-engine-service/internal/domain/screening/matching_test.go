package screening

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestMatchingAuthoring(t *testing.T) {
	base := AuthoringRule{ID: uuid.NewString(), InboxID: uuid.NewString(), Name: "Matching", EntityType: "Person", NameField: "name", Outcomes: []string{"approve"}}
	for _, threshold := range []int{0, 85, 100} {
		for _, limit := range []int{1, 50} {
			r := base
			r.Threshold = &threshold
			r.ResultLimit = &limit
			r.Datasets = []string{"synthetic", "other-test"}
			if err := r.Validate(); err != nil {
				t.Fatal(err)
			}
			got, ok := DecodeAuthoring(Config{ID: r.ID, Name: r.Name, Provider: "opensanctions", ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID})
			if !ok || *got.Threshold != threshold || *got.ResultLimit != limit || len(got.Datasets) != 2 {
				t.Fatal(got, ok)
			}
		}
	}
	for _, value := range []int{-1, 101} {
		r := base
		r.Threshold = &value
		if r.Validate() == nil {
			t.Fatal("invalid threshold")
		}
	}
	for _, value := range []int{0, 51} {
		r := base
		r.ResultLimit = &value
		if r.Validate() == nil {
			t.Fatal("invalid limit")
		}
	}
	for _, datasets := range [][]string{{""}, {"test", "test"}, {"a b"}, {"../x"}, {strings.Repeat("x", 101)}, make([]string, 21)} {
		r := base
		r.Datasets = datasets
		if r.Validate() == nil {
			t.Fatal("invalid datasets", datasets)
		}
	}
}

func TestScreeningConditionRoundtrip(t *testing.T) {
	r := AuthoringRule{ID: uuid.NewString(), InboxID: uuid.NewString(), Name: "Conditional", EntityType: "Person", NameField: "name", Outcomes: []string{"approve"}, Condition: &workflow.ConditionTree{Kind: "all", Children: []workflow.ConditionTree{{Kind: "payload", Field: "name", Operator: "contains", Value: []byte(`"Synthetic"`)}}}}
	got, ok := DecodeAuthoring(Config{ID: r.ID, Name: r.Name, Provider: "opensanctions", ConfigJSON: r.ConfigJSON(), AllowedOutcomes: r.Outcomes, ReviewInboxID: &r.InboxID})
	if !ok || got.Condition == nil || !got.Condition.Matches("approve", map[string]any{"name": "Synthetic Person"}) || got.Condition.Matches("approve", nil) {
		t.Fatal(got, ok)
	}
	r.Condition = &workflow.ConditionTree{Kind: "all"}
	if r.Validate() == nil {
		t.Fatal("empty condition accepted")
	}
}
