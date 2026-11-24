package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const OutboxEventQueueName = "outbox_events"

type OutboxEventArgs struct {
	Origin   platformauth.Origin `json:"originator"`
	TenantID string              `json:"tenant_id"`
	EventID  string              `json:"event_id"`
}

func (OutboxEventArgs) Kind() string {
	return "outbox_event"
}
