package riverjobs

const AsyncDecisionExecutionCallbackQueueName = "async_decision_execution_callbacks"

type AsyncDecisionExecutionCallbackArgs struct {
	TenantID    string `json:"tenant_id"`
	ExecutionID string `json:"execution_id"`
}

func (AsyncDecisionExecutionCallbackArgs) Kind() string {
	return "async_decision_execution_callback"
}
