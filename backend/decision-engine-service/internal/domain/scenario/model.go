package scenario

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type IterationStatus string

const (
	IterationStatusDraft     IterationStatus = "draft"
	IterationStatusCommitted IterationStatus = "committed"
)

type Scenario struct {
	ID                string
	TenantID          string
	Name              string
	Description       string
	TriggerObjectType string
	LiveIterationID   *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (s Scenario) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(s.TriggerObjectType) == "" {
		return fmt.Errorf("trigger_object_type is required")
	}
	return nil
}

type Iteration struct {
	ID                           string
	ScenarioID                   string
	TenantID                     string
	Version                      int
	Status                       IterationStatus
	TriggerFormula               json.RawMessage
	ScoreReviewThreshold         *int
	ScoreBlockAndReviewThreshold *int
	ScoreDeclineThreshold        *int
	Schedule                     string
	CreatedAt                    time.Time
	CommittedAt                  *time.Time
}

func (i Iteration) Validate() error {
	if strings.TrimSpace(i.ScenarioID) == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if strings.TrimSpace(i.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	switch i.Status {
	case IterationStatusDraft, IterationStatusCommitted:
	default:
		return fmt.Errorf("invalid iteration status %q", i.Status)
	}
	if hasThresholds(i) {
		if *i.ScoreBlockAndReviewThreshold < *i.ScoreReviewThreshold ||
			*i.ScoreDeclineThreshold < *i.ScoreBlockAndReviewThreshold {
			return fmt.Errorf("thresholds must satisfy review <= block_and_review <= decline")
		}
	}
	return nil
}

func hasThresholds(i Iteration) bool {
	return i.ScoreReviewThreshold != nil &&
		i.ScoreBlockAndReviewThreshold != nil &&
		i.ScoreDeclineThreshold != nil
}

type Rule struct {
	ID              string
	IterationID     string
	TenantID        string
	DisplayOrder    int
	Name            string
	Description     string
	Formula         json.RawMessage
	ScoreModifier   int
	RuleGroup       string
	SnoozeGroupID   *string
	StableRuleID    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (r Rule) Validate() error {
	if strings.TrimSpace(r.IterationID) == "" {
		return fmt.Errorf("iteration_id is required")
	}
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
