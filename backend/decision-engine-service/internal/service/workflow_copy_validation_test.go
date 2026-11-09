package service

import (
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestWorkflowCopyRuleHitValidation(t *testing.T) {
	for _, test := range []struct {
		name, params string
		valid        bool
	}{
		{"plural", `{"rule_ids":["included"]}`, true},
		{"legacy", `{"rule_id":["included"]}`, true},
		{"empty", `{}`, false},
		{"foreign", `{"rule_ids":["foreign"]}`, false},
		{"hidden foreign fallback", `{"rule_ids":["included"],"rule_id":["foreign"]}`, false},
		{"malformed", `{"rule_ids":"included"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			condition := workflow.Condition{TenantID: "tenant", RuleID: "workflow", Function: workflow.ConditionRuleHit, Params: json.RawMessage(test.params)}
			err := ValidateWorkflowCopyCondition(scenario.Scenario{}, condition, ports.TenantModel{}, map[string]bool{"included": true})
			if (err == nil) != test.valid {
				t.Fatal(err)
			}
		})
	}
}
