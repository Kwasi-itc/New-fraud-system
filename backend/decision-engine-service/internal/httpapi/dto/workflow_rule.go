package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type CreateWorkflowRuleRequest struct {
	Name        string `json:"name"`
	Fallthrough bool   `json:"fallthrough"`
}

type UpdateWorkflowRuleRequest struct {
	Name        string `json:"name"`
	Fallthrough bool   `json:"fallthrough"`
}

type WorkflowConditionRequest struct {
	Function string          `json:"function"`
	Params   json.RawMessage `json:"params"`
}

type WorkflowActionRequest struct {
	ActionType   string          `json:"action_type"`
	ActionConfig json.RawMessage `json:"action_config"`
}

type WorkflowRuleResponse struct {
	ID          string                      `json:"id"`
	TenantID    string                      `json:"tenant_id"`
	ScenarioID  string                      `json:"scenario_id"`
	Name        string                      `json:"name"`
	Priority    int                         `json:"priority"`
	Fallthrough bool                        `json:"fallthrough"`
	Conditions  []WorkflowConditionResponse `json:"conditions"`
	Actions     []WorkflowActionResponse    `json:"actions"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
}

type WorkflowConditionResponse struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	RuleID    string          `json:"rule_id"`
	Function  string          `json:"function"`
	Params    json.RawMessage `json:"params"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type WorkflowActionResponse struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	RuleID       string          `json:"rule_id"`
	ActionType   string          `json:"action_type"`
	ActionConfig json.RawMessage `json:"action_config"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func AdaptStructuredWorkflow(item workflow.StructuredRule) WorkflowRuleResponse {
	out := WorkflowRuleResponse{
		ID:          item.Rule.ID,
		TenantID:    item.Rule.TenantID,
		ScenarioID:  item.Rule.ScenarioID,
		Name:        item.Rule.Name,
		Priority:    item.Rule.Priority,
		Fallthrough: item.Rule.Fallthrough,
		Conditions:  make([]WorkflowConditionResponse, len(item.Conditions)),
		Actions:     make([]WorkflowActionResponse, len(item.Actions)),
		CreatedAt:   item.Rule.CreatedAt,
		UpdatedAt:   item.Rule.UpdatedAt,
	}
	for i, cond := range item.Conditions {
		out.Conditions[i] = AdaptWorkflowConditionRecord(cond)
	}
	for i, action := range item.Actions {
		out.Actions[i] = AdaptWorkflowActionRecord(action)
	}
	return out
}

func AdaptWorkflowConditionRecord(item workflow.Condition) WorkflowConditionResponse {
	return WorkflowConditionResponse{
		ID:        item.ID,
		TenantID:  item.TenantID,
		RuleID:    item.RuleID,
		Function:  string(item.Function),
		Params:    item.Params,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func AdaptWorkflowActionRecord(item workflow.Action) WorkflowActionResponse {
	return WorkflowActionResponse{
		ID:           item.ID,
		TenantID:     item.TenantID,
		RuleID:       item.RuleID,
		ActionType:   string(item.ActionType),
		ActionConfig: item.ActionConfig,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
	}
}
