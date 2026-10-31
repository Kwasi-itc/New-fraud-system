package riverjobs

import "github.com/google/uuid"

const DeferredIngestQueueName = "deferred_ingests"

type DeferredIngestArgs struct {
	DeferredIngestID uuid.UUID `json:"deferred_ingest_id"`
	PayloadVersion   int       `json:"payload_version,omitempty"`
	RetryAttempt     int       `json:"retry_attempt,omitempty"`
}

func (DeferredIngestArgs) Kind() string {
	return "deferred_ingest_execute"
}
