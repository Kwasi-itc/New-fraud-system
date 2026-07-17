package riverjobs

const OutboxEventQueueName = "outbox_events"

type OutboxEventArgs struct {
	TenantID string `json:"tenant_id"`
	EventID  string `json:"event_id"`
}

func (OutboxEventArgs) Kind() string {
	return "outbox_event"
}
