package riverjobs

const ScreeningExecutionQueueName = "screening_executions"

type ScreeningExecutionArgs struct {
	TenantID    string `json:"tenant_id"`
	ExecutionID string `json:"execution_id"`
}

func (ScreeningExecutionArgs) Kind() string {
	return "screening_execution"
}
