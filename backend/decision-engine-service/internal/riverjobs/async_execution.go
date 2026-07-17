package riverjobs

const AsyncDecisionExecutionQueueName = "async_decision_executions"

type AsyncDecisionExecutionArgs struct {
	ExecutionID string `json:"execution_id"`
}

func (AsyncDecisionExecutionArgs) Kind() string {
	return "async_decision_execution"
}
