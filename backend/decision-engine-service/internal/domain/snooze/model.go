package snooze

import (
	"fmt"
	"strings"
	"time"
)

type RuleSnooze struct {
	ID            string
	TenantID      string
	ScenarioID    string
	ObjectType    string
	ObjectID      string
	SnoozeGroupID string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

func (s RuleSnooze) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(s.ScenarioID) == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if strings.TrimSpace(s.ObjectType) == "" {
		return fmt.Errorf("object_type is required")
	}
	if strings.TrimSpace(s.ObjectID) == "" {
		return fmt.Errorf("object_id is required")
	}
	if strings.TrimSpace(s.SnoozeGroupID) == "" {
		return fmt.Errorf("snooze_group_id is required")
	}
	if !s.ExpiresAt.After(s.CreatedAt) {
		return fmt.Errorf("expires_at must be after created_at")
	}
	return nil
}
