package riverjobs

import (
	"context"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
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
	ctx = platformauth.WithOrigin(ctx, job.Args.Origin)
	return w.runner.RunScreeningExecution(ctx, job.Args.TenantID, job.Args.ExecutionID)
}

func (w *ScreeningExecutionWorker) Timeout(*river.Job[ScreeningExecutionArgs]) time.Duration {
	return 5 * time.Minute
}
