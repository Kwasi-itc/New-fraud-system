package scoring

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type RequestStatus string

const (
	RequestStatusPending   RequestStatus = "pending"
	RequestStatusQueued    RequestStatus = "queued"
	RequestStatusSent      RequestStatus = "sent"
	RequestStatusCompleted RequestStatus = "completed"
	RequestStatusFailed    RequestStatus = "failed"
)

type Config struct {
	ID              string
	TenantID        string
	ScenarioID      string
	Name            string
	AllowedOutcomes []string
	RulesetRef      string
	ConfigJSON      json.RawMessage
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(c.ScenarioID) == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(c.RulesetRef) == "" {
		return fmt.Errorf("ruleset_ref is required")
	}
	if len(c.AllowedOutcomes) == 0 {
		return fmt.Errorf("allowed_outcomes is required")
	}
	return nil
}

type Request struct {
	ID                string
	TenantID          string
	ConfigID          string
	DecisionID        string
	ScenarioID        string
	Status            RequestStatus
	RequestJSON       json.RawMessage
	ResponseJSON      json.RawMessage
	ProviderReference string
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SentAt            *time.Time
	CompletedAt       *time.Time
	FailedAt          *time.Time
}
