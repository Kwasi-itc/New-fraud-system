package execution

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type ScheduledExecution struct {
	ID                  string
	TenantID            string
	ScenarioID          string
	ScenarioIterationID string
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
