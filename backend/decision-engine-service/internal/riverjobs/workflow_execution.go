package riverjobs

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

const WorkflowExecutionQueueName = "workflow_executions"

type WorkflowExecutionArgs struct {
	Origin      platformauth.Origin `json:"originator"`
	TenantID    string              `json:"tenant_id"`
	ExecutionID string              `json:"execution_id"`
}

func (WorkflowExecutionArgs) Kind() string {
	return "workflow_execution"
}
