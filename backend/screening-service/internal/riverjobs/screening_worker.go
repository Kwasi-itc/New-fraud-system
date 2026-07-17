package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

type ScreeningRunner interface {
	RunScreening(ctx context.Context, tenantID, screeningID string) error
}

type ScreeningWorker struct {
	river.WorkerDefaults[ScreeningArgs]

	runner ScreeningRunner
}

func NewScreeningWorker(runner ScreeningRunner) ScreeningWorker {
	return ScreeningWorker{runner: runner}
}

func (w *ScreeningWorker) Work(ctx context.Context, job *river.Job[ScreeningArgs]) error {
	return w.runner.RunScreening(ctx, job.Args.TenantID, job.Args.ScreeningID)
}

func (w *ScreeningWorker) Timeout(*river.Job[ScreeningArgs]) time.Duration {
	return 5 * time.Minute
}
