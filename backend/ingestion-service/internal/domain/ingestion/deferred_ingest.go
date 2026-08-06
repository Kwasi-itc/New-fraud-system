package ingestion

import "time"

type DeferredIngestStatus string

const (
	DeferredIngestStatusQueued     DeferredIngestStatus = "queued"
	DeferredIngestStatusProcessing DeferredIngestStatus = "processing"
	DeferredIngestStatusCompleted  DeferredIngestStatus = "completed"
	DeferredIngestStatusFailed     DeferredIngestStatus = "failed"
)

type DeferredIngest struct {
	ID             string
	TenantID       string
	ObjectType     string
	Mode           Mode
	Status         DeferredIngestStatus
	AttemptCount   int
	ErrorMessage   *string
	IdempotencyKey *string
	Payload        []byte
	RequestedAt    time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

type DeferredIngestMetrics struct {
	QueuedCount                int        `json:"queued_count"`
	ProcessingCount            int        `json:"processing_count"`
	CompletedCount             int        `json:"completed_count"`
	FailedCount                int        `json:"failed_count"`
	RetryPendingCount          int        `json:"retry_pending_count"`
	RetriedExecutionCount      int        `json:"retried_execution_count"`
	OldestQueuedRequestedAt    *time.Time `json:"oldest_queued_requested_at,omitempty"`
	OldestQueuedAgeSeconds     float64    `json:"oldest_queued_age_seconds"`
	RecentSuccessCount         int        `json:"recent_success_count"`
	RecentFailureCount         int        `json:"recent_failure_count"`
	RecentRetryCount           int        `json:"recent_retry_count"`
	DrainRatePerMinuteLast5Min float64    `json:"drain_rate_per_minute_last_5m"`
	SnapshotAt                 time.Time  `json:"snapshot_at"`
}
