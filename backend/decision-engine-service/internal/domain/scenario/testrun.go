package scenario

import "time"

type TestRunStatus string

const (
	TestRunStatusPending TestRunStatus = "pending"
	TestRunStatusUp      TestRunStatus = "up"
	TestRunStatusDown    TestRunStatus = "down"
)

type TestRun struct {
	ID                string
	TenantID          string
	ScenarioID        string
	LiveIterationID   string
	PhantomIterationID string
	Status            TestRunStatus
	CreatedAt         time.Time
	ExpiresAt         time.Time
	UpdatedAt         time.Time
}
