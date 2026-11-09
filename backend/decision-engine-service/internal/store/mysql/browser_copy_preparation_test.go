package mysql

import (
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestCopiedBrowserConditionsUseDestinationModel(t *testing.T) {
	table := ports.TenantModelTable{Fields: map[string]ports.TenantModelField{"amount": {Type: "int"}, "label": {Type: "string"}, "old": {Type: "int", Archived: true}}}
	for _, tc := range []struct {
		name, field, operator, value string
		valid                        bool
	}{
		{"number", "amount", "gte", "10", true}, {"text", "label", "contains", `"x"`, true},
		{"missing", "missing", "eq", "1", false}, {"archived", "old", "eq", "1", false},
		{"wrong numeric type", "label", "gt", "1", false}, {"wrong text type", "amount", "contains", `"1"`, false},
		{"numeric equality", "amount", "eq", "9007199254740993", true}, {"incompatible equality", "label", "eq", "true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := workflow.ConditionTree{Kind: "all", Children: []workflow.ConditionTree{{Kind: "payload", Field: tc.field, Operator: tc.operator, Value: json.RawMessage(tc.value)}}}
			if err := validateCopiedCondition(tree, table); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
