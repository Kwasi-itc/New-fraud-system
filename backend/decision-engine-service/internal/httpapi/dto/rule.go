package dto

import (
	"encoding/json"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type CreateRuleRequest struct {
	DisplayOrder  int             `json:"display_order"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Formula       json.RawMessage `json:"formula"`
	ScoreModifier int             `json:"score_modifier"`
	RuleGroup     string          `json:"rule_group"`
	SnoozeGroupID *string         `json:"snooze_group_id"`
	StableRuleID  string          `json:"stable_rule_id"`
}

type UpdateRuleRequest struct {
	DisplayOrder  int             `json:"display_order"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Formula       json.RawMessage `json:"formula"`
	ScoreModifier int             `json:"score_modifier"`
	RuleGroup     string          `json:"rule_group"`
	SnoozeGroupID *string         `json:"snooze_group_id"`
	StableRuleID  string          `json:"stable_rule_id"`
}

type RuleResponse struct {
	ID            string          `json:"id"`
	IterationID   string          `json:"iteration_id"`
	TenantID      string          `json:"tenant_id"`
	DisplayOrder  int             `json:"display_order"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Formula       json.RawMessage `json:"formula"`
	ScoreModifier int             `json:"score_modifier"`
	RuleGroup     string          `json:"rule_group"`
	SnoozeGroupID *string         `json:"snooze_group_id,omitempty"`
	StableRuleID  string          `json:"stable_rule_id"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func AdaptRule(r scenario.Rule) RuleResponse {
	return RuleResponse{
		ID:            r.ID,
		IterationID:   r.IterationID,
		TenantID:      r.TenantID,
		DisplayOrder:  r.DisplayOrder,
		Name:          r.Name,
		Description:   r.Description,
		Formula:       r.Formula,
		ScoreModifier: r.ScoreModifier,
		RuleGroup:     r.RuleGroup,
		SnoozeGroupID: r.SnoozeGroupID,
		StableRuleID:  r.StableRuleID,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}
