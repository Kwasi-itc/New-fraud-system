package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const AsyncDecisionExecutionCallbackQueueName = "async_decision_execution_callbacks"

type AsyncDecisionExecutionCallbackArgs struct {
	Origin      platformauth.Origin `json:"originator"`
	TenantID    string              `json:"tenant_id"`
	ExecutionID string              `json:"execution_id"`
}

func (AsyncDecisionExecutionCallbackArgs) Kind() string {
	return "async_decision_execution_callback"
}
