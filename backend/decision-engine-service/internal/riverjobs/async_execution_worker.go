package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

type AsyncDecisionExecutionRunner interface {
	RunAsyncDecisionExecution(ctx context.Context, executionID string) error
}

type AsyncDecisionExecutionWorker struct {
	river.WorkerDefaults[AsyncDecisionExecutionArgs]

	runner AsyncDecisionExecutionRunner
}

func NewAsyncDecisionExecutionWorker(runner AsyncDecisionExecutionRunner) AsyncDecisionExecutionWorker {
	return AsyncDecisionExecutionWorker{runner: runner}
}

func (w *AsyncDecisionExecutionWorker) Work(ctx context.Context, job *river.Job[AsyncDecisionExecutionArgs]) error {
	return w.runner.RunAsyncDecisionExecution(ctx, job.Args.ExecutionID)
}

func (w *AsyncDecisionExecutionWorker) Timeout(*river.Job[AsyncDecisionExecutionArgs]) time.Duration {
	return 10 * time.Minute
}
