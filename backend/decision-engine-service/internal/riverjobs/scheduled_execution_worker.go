package riverjobs

import (
	"context"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"time"

	"github.com/riverqueue/river"
)

type ScheduledExecutionRunner interface {
	RunScheduledExecution(ctx context.Context, executionID string) error
}

type ScheduledExecutionWorker struct {
	river.WorkerDefaults[ScheduledExecutionArgs]

	runner ScheduledExecutionRunner
}

func NewScheduledExecutionWorker(runner ScheduledExecutionRunner) ScheduledExecutionWorker {
	return ScheduledExecutionWorker{runner: runner}
}

func (w *ScheduledExecutionWorker) Work(ctx context.Context, job *river.Job[ScheduledExecutionArgs]) error {
	ctx = platformauth.WithOrigin(ctx, job.Args.Origin)
	return w.runner.RunScheduledExecution(ctx, job.Args.ExecutionID)
}

func (w *ScheduledExecutionWorker) Timeout(*river.Job[ScheduledExecutionArgs]) time.Duration {
	return 10 * time.Minute
}
