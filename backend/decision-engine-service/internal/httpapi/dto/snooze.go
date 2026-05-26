package dto

import (
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
)

type CreateRuleSnoozeRequest struct {
	ObjectType    string    `json:"object_type"`
	ObjectID      string    `json:"object_id"`
	SnoozeGroupID string    `json:"snooze_group_id"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type RuleSnoozeResponse struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	ScenarioID    string    `json:"scenario_id"`
	ObjectType    string    `json:"object_type"`
	ObjectID      string    `json:"object_id"`
	SnoozeGroupID string    `json:"snooze_group_id"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func AdaptRuleSnooze(item snooze.RuleSnooze) RuleSnoozeResponse {
	return RuleSnoozeResponse{
		ID:            item.ID,
		TenantID:      item.TenantID,
		ScenarioID:    item.ScenarioID,
		ObjectType:    item.ObjectType,
		ObjectID:      item.ObjectID,
		SnoozeGroupID: item.SnoozeGroupID,
		CreatedAt:     item.CreatedAt,
		ExpiresAt:     item.ExpiresAt,
	}
}
