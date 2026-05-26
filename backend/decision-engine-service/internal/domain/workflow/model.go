package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ActionType string

const (
	ActionTypeCreateCase ActionType = "create_case"
	ActionTypeAddTag     ActionType = "add_tag"
	ActionTypeEmitEvent  ActionType = "emit_event"
)

type ExecutionStatus string

const (
	ExecutionStatusPendingDispatch ExecutionStatus = "pending_dispatch"
	ExecutionStatusDispatched      ExecutionStatus = "dispatched"
	ExecutionStatusDispatchFailed  ExecutionStatus = "dispatch_failed"
)

type ConditionType string

const (
	ConditionAlways           ConditionType = "always"
	ConditionNever            ConditionType = "never"
	ConditionOutcomeIn        ConditionType = "outcome_in"
	ConditionRuleHit          ConditionType = "rule_hit"
	ConditionPayloadEvaluates ConditionType = "payload_evaluates"
)

type Definition struct {
	ID              string
	TenantID        string
	ScenarioID      string
	DisplayOrder    int
	Name            string
	Description     string
	AllowedOutcomes []string
	ActionType      ActionType
	ActionConfig    json.RawMessage
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (d Definition) Validate() error {
	if strings.TrimSpace(d.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(d.ScenarioID) == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(d.AllowedOutcomes) == 0 {
		return fmt.Errorf("allowed_outcomes is required")
	}
	switch d.ActionType {
	case ActionTypeCreateCase, ActionTypeAddTag, ActionTypeEmitEvent:
	default:
		return fmt.Errorf("invalid action_type %q", d.ActionType)
	}
	return nil
}

type Execution struct {
	ID               string
	TenantID         string
	WorkflowID       *string
	WorkflowRuleID   *string
	WorkflowActionID *string
	DecisionID       string
	ScenarioID       string
	ActionType       ActionType
	Status           ExecutionStatus
	ActionConfig     json.RawMessage
	CreatedAt        time.Time
}

type Rule struct {
	ID          string
	TenantID    string
	ScenarioID  string
	Name        string
	Priority    int
	Fallthrough bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r Rule) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(r.ScenarioID) == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

type Condition struct {
	ID        string
	TenantID  string
	RuleID    string
	Function  ConditionType
	Params    json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c Condition) Validate() error {
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(c.RuleID) == "" {
		return fmt.Errorf("rule_id is required")
	}
	switch c.Function {
	case ConditionAlways, ConditionNever, ConditionOutcomeIn, ConditionRuleHit, ConditionPayloadEvaluates:
	default:
		return fmt.Errorf("invalid condition function %q", c.Function)
	}
	return nil
}

type Action struct {
	ID           string
	TenantID     string
	RuleID       string
	ActionType   ActionType
	ActionConfig json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (a Action) Validate() error {
	if strings.TrimSpace(a.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(a.RuleID) == "" {
		return fmt.Errorf("rule_id is required")
	}
	switch a.ActionType {
	case ActionTypeCreateCase, ActionTypeAddTag, ActionTypeEmitEvent:
	default:
		return fmt.Errorf("invalid action_type %q", a.ActionType)
	}
	return nil
}

type StructuredRule struct {
	Rule       Rule
	Conditions []Condition
	Actions    []Action
}
