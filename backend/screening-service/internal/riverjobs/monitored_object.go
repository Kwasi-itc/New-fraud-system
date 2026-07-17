package riverjobs

const MonitoredObjectQueueName = "monitored_objects"

type MonitoredObjectArgs struct {
	TenantID          string `json:"tenant_id"`
	MonitoredObjectID string `json:"monitored_object_id"`
}

func (MonitoredObjectArgs) Kind() string {
	return "monitored_object"
}
