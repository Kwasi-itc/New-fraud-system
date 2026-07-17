package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

type AsyncDecisionExecutionCallbackRunner interface {
	DeliverAsyncExecutionCallback(ctx context.Context, tenantID, executionID string) error
}

type AsyncDecisionExecutionCallbackWorker struct {
	river.WorkerDefaults[AsyncDecisionExecutionCallbackArgs]

	runner AsyncDecisionExecutionCallbackRunner
}

func NewAsyncDecisionExecutionCallbackWorker(runner AsyncDecisionExecutionCallbackRunner) AsyncDecisionExecutionCallbackWorker {
	return AsyncDecisionExecutionCallbackWorker{runner: runner}
}

func (w *AsyncDecisionExecutionCallbackWorker) Work(ctx context.Context, job *river.Job[AsyncDecisionExecutionCallbackArgs]) error {
	return w.runner.DeliverAsyncExecutionCallback(ctx, job.Args.TenantID, job.Args.ExecutionID)
}

func (w *AsyncDecisionExecutionCallbackWorker) Timeout(*river.Job[AsyncDecisionExecutionCallbackArgs]) time.Duration {
	return 2 * time.Minute
}
