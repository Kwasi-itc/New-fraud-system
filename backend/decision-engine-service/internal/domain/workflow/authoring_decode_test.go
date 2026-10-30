package workflow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDecodeCaseRuleRecognizesOnlyBrowserFormats(t *testing.T) {
	id, inbox := uuid.NewString(), uuid.NewString()
	at := time.Now().UTC().Truncate(time.Microsecond)
	config, err := json.Marshal(map[string]any{"authoring_version": 1, "inbox_id": inbox, "name": "Review", "tag_ids": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	v := StructuredRule{Rule: Rule{ID: id, Name: "Funding review", UpdatedAt: at}, Conditions: []Condition{{Function: ConditionOutcomeIn, Params: json.RawMessage(`["review"]`)}}, Actions: []Action{{ActionType: ActionTypeCreateCase, ActionConfig: config}}}
	got, ok := DecodeCaseRule(v)
	if !ok || !got.Enabled || got.ID != id || got.InboxID != inbox || got.UpdatedAt == nil || !got.UpdatedAt.Equal(at) || got.TagIDs == nil {
		t.Fatal("v1 decode", got, ok)
	}
	v.Conditions[0].Function = ConditionNever
	if got, ok := DecodeCaseRule(v); !ok || got.Enabled {
		t.Fatal("disabled v1", got, ok)
	}
	config, err = json.Marshal(map[string]any{"authoring_version": 2, "inbox_id": inbox, "name": "Review"})
	if err != nil {
		t.Fatal(err)
	}
	v.Actions[0].ActionConfig = config
	v.Conditions[0] = Condition{Function: ConditionTreeMatch, Params: json.RawMessage(`{"kind":"payload","field":"amount","operator":"gt","value":50}`)}
	if got, ok := DecodeCaseRule(v); !ok || got.Condition == nil || got.Condition.Field != "amount" || len(got.Outcomes) != 0 {
		t.Fatal("v2 decode", got, ok)
	}
	for _, modify := range []func(*StructuredRule){
		func(v *StructuredRule) { v.Rule.Fallthrough = true },
		func(v *StructuredRule) { v.Actions = append(v.Actions, v.Actions[0]) },
		func(v *StructuredRule) { v.Conditions = nil },
		func(v *StructuredRule) { v.Conditions[0].Function = ConditionAlways },
		func(v *StructuredRule) { v.Conditions[0].Params = json.RawMessage(`null`) },
		func(v *StructuredRule) { v.Actions[0].ActionConfig = json.RawMessage(`{"authoring_version":3}`) },
		func(v *StructuredRule) { v.Actions[0].ActionType = ActionType("unknown") },
	} {
		copy := v
		copy.Conditions = append([]Condition(nil), v.Conditions...)
		copy.Actions = append([]Action(nil), v.Actions...)
		modify(&copy)
		if _, ok := DecodeCaseRule(copy); ok {
			t.Fatal("arbitrary/invalid structured workflow accepted")
		}
	}
}
