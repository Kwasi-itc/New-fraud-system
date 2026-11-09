package service

import (
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// ValidateWorkflowCopyCondition performs no IO and must run before copying.
// Rule-hit identities must belong to the graph that will be reidentified.
func ValidateWorkflowCopyCondition(parent scenario.Scenario, item workflow.Condition, model ports.TenantModel, ruleIDs map[string]bool) error {
	if err := item.Validate(); err != nil {
		return err
	}
	switch item.Function {
	case workflow.ConditionAlways, workflow.ConditionNever:
		return nil // These evaluators ignore params, including legacy empty objects.
	case workflow.ConditionTreeMatch:
		var tree workflow.ConditionTree
		if err := json.Unmarshal(item.Params, &tree); err != nil {
			return err
		}
		return tree.Validate()
	case workflow.ConditionOutcomeIn:
		var outcomes []string
		if err := json.Unmarshal(item.Params, &outcomes); err != nil {
			return err
		}
		return (workflow.ConditionTree{Kind: "outcome_in", Outcomes: outcomes}).Validate()
	case workflow.ConditionRuleHit:
		var params ruleHitParams
		if err := json.Unmarshal(item.Params, &params); err != nil {
			return err
		}
		if len(params.IDs()) == 0 {
			return fmt.Errorf("rule-hit condition requires rule identities")
		}
		for _, id := range append(params.RuleID, params.RuleIDs...) {
			if !ruleIDs[id] {
				return fmt.Errorf("rule-hit reference is outside the copied graph")
			}
		}
		return nil
	case workflow.ConditionPayloadEvaluates:
		var params evaluatesParams
		if err := json.Unmarshal(item.Params, &params); err != nil {
			return err
		}
		return validateScenarioModel(parent, scenario.Iteration{TriggerFormula: params.Expression}, nil, model)
	default:
		return fmt.Errorf("unsupported copied workflow condition")
	}
}
