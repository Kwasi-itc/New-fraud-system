package riverjobs

import (
	"context"
	"errors"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
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
	ctx = platformauth.WithOrigin(ctx, job.Args.Origin)
	err := w.runner.RunScreening(ctx, job.Args.TenantID, job.Args.ScreeningID)
	if errors.Is(err, ports.ErrScreeningBusy) {
		return river.JobSnooze(time.Minute)
	}
	return err
}

func (w *ScreeningWorker) Timeout(*river.Job[ScreeningArgs]) time.Duration {
	return 5 * time.Minute
}
