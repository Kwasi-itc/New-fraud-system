package execution

import (
	"encoding/json"
	"time"
)

type Status string
type Source string

const (
	StatusPending   Status = "pending"
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

const (
	SourceManual    Source = "manual"
	SourceRecurring Source = "recurring"
)

type ScheduledExecution struct {
	ID                  string
	TenantID            string
	ScenarioID          string
	ScenarioIterationID string
	Source              Source
	Status              Status
	IdempotencyKey      string
	AttemptCount        int
	MaxAttempts         int
	ScheduledFor        time.Time
	NextAttemptAt       *time.Time
	RequestBody         json.RawMessage
	LastError           string
	CreatedAt           time.Time
	FailedAt            *time.Time
}

type AsyncDecisionExecution struct {
	ID             string
	TenantID       string
	ScenarioID     string
	ObjectType     string
	Status         Status
	IdempotencyKey string
	AttemptCount   int
	MaxAttempts    int
	NextAttemptAt  *time.Time
	RequestBody    json.RawMessage
	LastError      string
	CreatedAt      time.Time
	FailedAt       *time.Time
}
