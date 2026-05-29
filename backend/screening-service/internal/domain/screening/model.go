package screening

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusPending        Status = "pending"
	StatusQueued         Status = "queued"
	StatusProcessing     Status = "processing"
	StatusAwaitingReview Status = "awaiting_review"
	StatusNoHit          Status = "no_hit"
	StatusConfirmedHit   Status = "confirmed_hit"
	StatusFailed         Status = "failed"
	StatusArchived       Status = "archived"
)

type MatchStatus string

const (
	MatchStatusPending      MatchStatus = "pending"
	MatchStatusConfirmedHit MatchStatus = "confirmed_hit"
	MatchStatusNoHit        MatchStatus = "no_hit"
	MatchStatusSkipped      MatchStatus = "skipped"
)

type Screening struct {
	ID                           string
	TenantID                     string
	DecisionID                   string
	ScenarioID                   string
	ScreeningConfigID            string
	IdempotencyKey               string
	Provider                     string
	ObjectType                   string
	ObjectID                     string
	Status                       Status
	RequestJSON                  json.RawMessage
	ResponseJSON                 json.RawMessage
	ProviderReference            string
	LastError                    string
	IsManual                     bool
	IsArchived                   bool
	Partial                      bool
	UniqueCounterpartyIdentifier *string
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
	SentAt                       *time.Time
	CompletedAt                  *time.Time
	FailedAt                     *time.Time
}

func (s Screening) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(s.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(s.ObjectType) == "" {
		return fmt.Errorf("object_type is required")
	}
	if strings.TrimSpace(s.ObjectID) == "" {
		return fmt.Errorf("object_id is required")
	}
	if !s.Status.IsValid() {
		return fmt.Errorf("status %q is invalid", s.Status)
	}
	return nil
}

func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusQueued, StatusProcessing, StatusAwaitingReview, StatusNoHit, StatusConfirmedHit, StatusFailed, StatusArchived:
		return true
	default:
		return false
	}
}

type Match struct {
	ID                           string
	TenantID                     string
	ScreeningID                  string
	EntityID                     string
	Provider                     string
	Status                       MatchStatus
	Name                         string
	Score                        float64
	Payload                      json.RawMessage
	MatchedTexts                 []string
	UniqueCounterpartyIdentifier *string
	Enriched                     bool
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
	Comments                     []Comment
}

func (m Match) Validate() error {
	if strings.TrimSpace(m.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(m.ScreeningID) == "" {
		return fmt.Errorf("screening_id is required")
	}
	if strings.TrimSpace(m.EntityID) == "" {
		return fmt.Errorf("entity_id is required")
	}
	if strings.TrimSpace(m.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if !m.Status.IsValid() {
		return fmt.Errorf("match status %q is invalid", m.Status)
	}
	return nil
}

func (m MatchStatus) IsValid() bool {
	switch m {
	case MatchStatusPending, MatchStatusConfirmedHit, MatchStatusNoHit, MatchStatusSkipped:
		return true
	default:
		return false
	}
}

type Comment struct {
	ID          string
	TenantID    string
	MatchID     string
	CommentText string
	AuthorID    string
	CreatedAt   time.Time
}

type WhitelistEntry struct {
	ID                           string
	TenantID                     string
	EntityID                     string
	UniqueCounterpartyIdentifier *string
	ReviewerID                   string
	CreatedAt                    time.Time
}

type File struct {
	ID          string
	TenantID    string
	ScreeningID string
	FileName    string
	ContentType string
	FileSize    int64
	StorageKey  string
	UploadedBy  string
	CreatedAt   time.Time
}

type ContinuousConfig struct {
	ID            string
	TenantID      string
	Name          string
	ObjectType    string
	Provider      string
	FieldMapJSON  json.RawMessage
	ReviewInboxID *string
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (c ContinuousConfig) Validate() error {
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(c.ObjectType) == "" {
		return fmt.Errorf("object_type is required")
	}
	if strings.TrimSpace(c.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	return nil
}

type MonitoredObjectStatus string

const (
	MonitoredObjectStatusPending  MonitoredObjectStatus = "pending"
	MonitoredObjectStatusActive   MonitoredObjectStatus = "active"
	MonitoredObjectStatusPaused   MonitoredObjectStatus = "paused"
	MonitoredObjectStatusArchived MonitoredObjectStatus = "archived"
)

type MonitoredObject struct {
	ID             string
	TenantID       string
	ConfigID       string
	ObjectType     string
	ObjectID       string
	Status         MonitoredObjectStatus
	AttributesJSON json.RawMessage
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastScreenedAt *time.Time
}

func (m MonitoredObject) Validate() error {
	if strings.TrimSpace(m.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(m.ConfigID) == "" {
		return fmt.Errorf("config_id is required")
	}
	if strings.TrimSpace(m.ObjectType) == "" {
		return fmt.Errorf("object_type is required")
	}
	if strings.TrimSpace(m.ObjectID) == "" {
		return fmt.Errorf("object_id is required")
	}
	switch m.Status {
	case MonitoredObjectStatusPending, MonitoredObjectStatusActive, MonitoredObjectStatusPaused, MonitoredObjectStatusArchived:
		return nil
	default:
		return fmt.Errorf("invalid monitored object status %q", m.Status)
	}
}

type SearchQuery struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type SearchRequest struct {
	Provider                     string          `json:"provider"`
	DecisionID                   string          `json:"decision_id,omitempty"`
	ScenarioID                   string          `json:"scenario_id,omitempty"`
	ScreeningConfigID            string          `json:"screening_config_id,omitempty"`
	IdempotencyKey               string          `json:"idempotency_key,omitempty"`
	ObjectType                   string          `json:"object_type"`
	ObjectID                     string          `json:"object_id"`
	Queries                      []SearchQuery   `json:"queries"`
	ProviderConfig               json.RawMessage `json:"provider_config,omitempty"`
	LimitOverride                *int            `json:"limit_override,omitempty"`
	UniqueCounterpartyIdentifier *string         `json:"unique_counterparty_identifier,omitempty"`
	IsManual                     bool            `json:"is_manual,omitempty"`
}

type ProviderMatch struct {
	EntityID                     string          `json:"entity_id"`
	Name                         string          `json:"name"`
	Score                        float64         `json:"score"`
	Payload                      json.RawMessage `json:"payload"`
	MatchedTexts                 []string        `json:"matched_texts"`
	UniqueCounterpartyIdentifier *string         `json:"unique_counterparty_identifier,omitempty"`
}

type ProviderResult struct {
	ProviderReference string          `json:"provider_reference"`
	RawResponse       json.RawMessage `json:"raw_response"`
	Partial           bool            `json:"partial"`
	Matches           []ProviderMatch `json:"matches"`
}

type EnrichmentResult struct {
	RawPayload json.RawMessage `json:"raw_payload"`
}

type DatasetCatalog struct {
	RawPayload json.RawMessage `json:"raw_payload"`
}

type DatasetFreshness struct {
	RawPayload json.RawMessage `json:"raw_payload"`
}

type DatasetDelta struct {
	RawPayload json.RawMessage `json:"raw_payload"`
	NextCursor string          `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
	Changed    bool            `json:"changed"`
}

type DatasetUpdateJobStatus string

const (
	DatasetUpdateJobStatusPending    DatasetUpdateJobStatus = "pending"
	DatasetUpdateJobStatusProcessing DatasetUpdateJobStatus = "processing"
	DatasetUpdateJobStatusCompleted  DatasetUpdateJobStatus = "completed"
	DatasetUpdateJobStatusFailed     DatasetUpdateJobStatus = "failed"
)

type DatasetUpdateJob struct {
	ID           string
	TenantID     string
	Provider     string
	JobType      string
	Status       DatasetUpdateJobStatus
	Cursor       string
	ResultJSON   json.RawMessage
	LastError    string
	AttemptCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

func (j DatasetUpdateJob) Validate() error {
	if strings.TrimSpace(j.TenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(j.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(j.JobType) == "" {
		return fmt.Errorf("job_type is required")
	}
	switch j.Status {
	case DatasetUpdateJobStatusPending, DatasetUpdateJobStatusProcessing, DatasetUpdateJobStatusCompleted, DatasetUpdateJobStatusFailed:
		return nil
	default:
		return fmt.Errorf("invalid dataset update job status %q", j.Status)
	}
}

type Details struct {
	Screening Screening `json:"screening"`
	Matches   []Match   `json:"matches"`
}
