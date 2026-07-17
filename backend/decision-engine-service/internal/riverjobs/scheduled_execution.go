package riverjobs

const ScheduledExecutionQueueName = "scheduled_executions"

type ScheduledExecutionArgs struct {
	ExecutionID string `json:"execution_id"`
}

func (ScheduledExecutionArgs) Kind() string {
	return "scheduled_execution"
}
