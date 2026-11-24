package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const AsyncDecisionExecutionQueueName = "async_decision_executions"

type AsyncDecisionExecutionArgs struct {
	Origin      platformauth.Origin `json:"originator"`
	ExecutionID string              `json:"execution_id"`
}

func (AsyncDecisionExecutionArgs) Kind() string {
	return "async_decision_execution"
}
