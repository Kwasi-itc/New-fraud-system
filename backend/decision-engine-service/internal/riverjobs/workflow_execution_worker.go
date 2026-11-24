package riverjobs

import (
	"context"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"time"

	"github.com/riverqueue/river"
)

type WorkflowExecutionRunner interface {
	RunWorkflowExecution(ctx context.Context, tenantID, executionID string) error
}

type WorkflowExecutionWorker struct {
	river.WorkerDefaults[WorkflowExecutionArgs]

	runner WorkflowExecutionRunner
}

func NewWorkflowExecutionWorker(runner WorkflowExecutionRunner) WorkflowExecutionWorker {
	return WorkflowExecutionWorker{runner: runner}
}

func (w *WorkflowExecutionWorker) Work(ctx context.Context, job *river.Job[WorkflowExecutionArgs]) error {
	ctx = platformauth.WithOrigin(ctx, job.Args.Origin)
	return w.runner.RunWorkflowExecution(ctx, job.Args.TenantID, job.Args.ExecutionID)
}

func (w *WorkflowExecutionWorker) Timeout(*river.Job[WorkflowExecutionArgs]) time.Duration {
	return 5 * time.Minute
}
