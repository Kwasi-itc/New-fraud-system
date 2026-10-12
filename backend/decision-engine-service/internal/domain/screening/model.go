package screening

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusQueued    ExecutionStatus = "queued"
	ExecutionStatusSent      ExecutionStatus = "sent"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
)

type Config struct {
	ID              string
	TenantID        string
	ScenarioID      string
	Name            string
	AllowedOutcomes []string
	Provider        string
	ConfigJSON      json.RawMessage
	Active          bool
	ReviewInboxID   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (c Config) Validate() error {
	if c.ReviewInboxID != nil {
		if id, err := uuid.Parse(*c.ReviewInboxID); err != nil || id == uuid.Nil {
			return fmt.Errorf("review_inbox_id must be a nonzero UUID")
		}
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(c.ScenarioID) == "" {
		return fmt.Errorf("scenario_id is required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(c.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if len(c.AllowedOutcomes) == 0 {
		return fmt.Errorf("allowed_outcomes is required")
	}
	return nil
}

type Execution struct {
	ID                string
	TenantID          string
	ConfigID          string
	DecisionID        string
	ScenarioID        string
	Status            ExecutionStatus
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
