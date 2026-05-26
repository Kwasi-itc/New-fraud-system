package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type CreateWorkflowRequest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	AllowedOutcomes []string        `json:"allowed_outcomes"`
	ActionType      string          `json:"action_type"`
	ActionConfig    json.RawMessage `json:"action_config"`
	Active          bool            `json:"active"`
}

type UpdateWorkflowRequest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	AllowedOutcomes []string        `json:"allowed_outcomes"`
	ActionType      string          `json:"action_type"`
	ActionConfig    json.RawMessage `json:"action_config"`
	Active          bool            `json:"active"`
}

type WorkflowResponse struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	ScenarioID      string          `json:"scenario_id"`
	DisplayOrder    int             `json:"display_order"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	AllowedOutcomes []string        `json:"allowed_outcomes"`
	ActionType      string          `json:"action_type"`
	ActionConfig    json.RawMessage `json:"action_config"`
	Active          bool            `json:"active"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type WorkflowExecutionResponse struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	WorkflowID       *string         `json:"workflow_id,omitempty"`
	WorkflowRuleID   *string         `json:"workflow_rule_id,omitempty"`
	WorkflowActionID *string         `json:"workflow_action_id,omitempty"`
	DecisionID       string          `json:"decision_id"`
	ScenarioID       string          `json:"scenario_id"`
	ActionType       string          `json:"action_type"`
	Status           string          `json:"status"`
	ActionConfig     json.RawMessage `json:"action_config"`
	CreatedAt        time.Time       `json:"created_at"`
}

func AdaptWorkflow(item workflow.Definition) WorkflowResponse {
	return WorkflowResponse{
		ID:              item.ID,
		TenantID:        item.TenantID,
		ScenarioID:      item.ScenarioID,
		DisplayOrder:    item.DisplayOrder,
		Name:            item.Name,
		Description:     item.Description,
		AllowedOutcomes: item.AllowedOutcomes,
		ActionType:      string(item.ActionType),
		ActionConfig:    item.ActionConfig,
		Active:          item.Active,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

func AdaptWorkflowExecution(item workflow.Execution) WorkflowExecutionResponse {
	return WorkflowExecutionResponse{
		ID:               item.ID,
		TenantID:         item.TenantID,
		WorkflowID:       item.WorkflowID,
		WorkflowRuleID:   item.WorkflowRuleID,
		WorkflowActionID: item.WorkflowActionID,
		DecisionID:       item.DecisionID,
		ScenarioID:       item.ScenarioID,
		ActionType:       string(item.ActionType),
		Status:           string(item.Status),
		ActionConfig:     item.ActionConfig,
		CreatedAt:        item.CreatedAt,
	}
}
