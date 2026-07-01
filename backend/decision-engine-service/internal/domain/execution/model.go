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
	ScheduledFor        time.Time
	RequestBody         json.RawMessage
	CreatedAt           time.Time
}

type AsyncDecisionExecution struct {
	ID          string
	TenantID    string
	ScenarioID  string
	ObjectType  string
	Status      Status
	RequestBody json.RawMessage
	CreatedAt   time.Time
}
