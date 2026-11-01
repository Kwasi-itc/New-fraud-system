package ports

import "time"

// JobKind is an owner operation, not a transport queue name. Adapters own queue
// configuration and payload formats; callers never receive a driver transaction.
type JobKind string

const (
	JobScheduled JobKind = "scheduled_execution"
	JobAsync     JobKind = "async_decision_execution"
	JobCallback  JobKind = "async_decision_execution_callback"
	JobWorkflow  JobKind = "workflow_execution"
	JobScreening JobKind = "screening_execution"
	JobScoring   JobKind = "scoring_request"
	JobOutbox    JobKind = "outbox_event"
)

// JobRequest references an existing owner resource, including rows created in
// the same transaction. TenantID is required even for legacy resource-only jobs.
// It does not authorize the caller or change domain attempt/lease policy.
type JobRequest struct {
	Kind        JobKind
	TenantID    string
	ResourceID  string
	ScheduledAt *time.Time
}
