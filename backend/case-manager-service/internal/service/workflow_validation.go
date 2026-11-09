package service

import (
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

// ValidateWorkflowInput is shared by source and staged target case owners.
func ValidateWorkflowInput(in WorkflowActionInput) error {
	if in.ActionConfig.URL != "" || len(in.ActionConfig.TitleTemplate) > 0 {
		return casepkg.Invalid("case actions accept literal name/title and no dispatcher URL or title_template")
	}
	if err := requireIDs(in.TenantID, in.WorkflowExecutionID, in.DecisionID, in.ActionConfig.InboxID); err != nil {
		return err
	}
	switch in.ActionType {
	case "create_case", "add_to_case", "add_to_case_if_possible":
	default:
		return casepkg.Invalid("unsupported case workflow action")
	}
	if in.ScenarioID != nil {
		if err := requireIDs(*in.ScenarioID); err != nil {
			return err
		}
	}
	if len(in.ActionConfig.TagIDs) > 500 {
		return casepkg.Invalid("at most 500 workflow tags are allowed")
	}
	for _, id := range in.ActionConfig.TagIDs {
		if id == uuid.Nil {
			return casepkg.Invalid("tag ID is required")
		}
	}
	return nil
}

func ValidateWorkflowDecision(in WorkflowActionInput, ref casepkg.DecisionReference) error {
	return validateDecisionMetadata(ref, in.ScenarioID, in.ActionConfig.ObjectType, in.ActionConfig.ObjectID)
}
