package riverjobs

const WorkflowExecutionQueueName = "workflow_executions"

type WorkflowExecutionArgs struct {
	TenantID    string `json:"tenant_id"`
	ExecutionID string `json:"execution_id"`
}

func (WorkflowExecutionArgs) Kind() string {
	return "workflow_execution"
}
