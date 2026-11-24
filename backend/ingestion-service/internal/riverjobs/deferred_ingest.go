package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

import "github.com/google/uuid"

const DeferredIngestQueueName = "deferred_ingests"

type DeferredIngestArgs struct {
	Origin           platformauth.Origin `json:"originator"`
	DeferredIngestID uuid.UUID           `json:"deferred_ingest_id"`
}

func (DeferredIngestArgs) Kind() string {
	return "deferred_ingest_execute"
}
