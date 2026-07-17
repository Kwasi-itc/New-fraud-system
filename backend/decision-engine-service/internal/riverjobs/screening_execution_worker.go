package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

type ScreeningExecutionRunner interface {
	RunScreeningExecution(ctx context.Context, tenantID, executionID string) error
}

type ScreeningExecutionWorker struct {
	river.WorkerDefaults[ScreeningExecutionArgs]

	runner ScreeningExecutionRunner
}

func NewScreeningExecutionWorker(runner ScreeningExecutionRunner) ScreeningExecutionWorker {
	return ScreeningExecutionWorker{runner: runner}
}

func (w *ScreeningExecutionWorker) Work(ctx context.Context, job *river.Job[ScreeningExecutionArgs]) error {
	return w.runner.RunScreeningExecution(ctx, job.Args.TenantID, job.Args.ExecutionID)
}

func (w *ScreeningExecutionWorker) Timeout(*river.Job[ScreeningExecutionArgs]) time.Duration {
	return 5 * time.Minute
}
