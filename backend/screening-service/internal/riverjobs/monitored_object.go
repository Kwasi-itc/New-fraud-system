package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const MonitoredObjectQueueName = "monitored_objects"

type MonitoredObjectArgs struct {
	Origin            platformauth.Origin `json:"originator"`
	TenantID          string              `json:"tenant_id"`
	MonitoredObjectID string              `json:"monitored_object_id"`
}

func (MonitoredObjectArgs) Kind() string {
	return "monitored_object"
}
