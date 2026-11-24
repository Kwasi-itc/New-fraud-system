package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const ScheduledExecutionQueueName = "scheduled_executions"

type ScheduledExecutionArgs struct {
	Origin      platformauth.Origin `json:"originator"`
	ExecutionID string              `json:"execution_id"`
}

func (ScheduledExecutionArgs) Kind() string {
	return "scheduled_execution"
}
