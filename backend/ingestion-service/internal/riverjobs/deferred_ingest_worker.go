package riverjobs

import (
	"context"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

type DeferredIngestRunner interface {
	RunDeferredIngest(ctx context.Context, id uuid.UUID) error
}

type DeferredIngestWorker struct {
	river.WorkerDefaults[DeferredIngestArgs]

	runner DeferredIngestRunner
}

func NewDeferredIngestWorker(runner DeferredIngestRunner) DeferredIngestWorker {
	return DeferredIngestWorker{runner: runner}
}

func (w *DeferredIngestWorker) Work(ctx context.Context, job *river.Job[DeferredIngestArgs]) error {
	ctx = platformauth.WithOrigin(ctx, job.Args.Origin)
	return w.runner.RunDeferredIngest(ctx, job.Args.DeferredIngestID)
}

func (w *DeferredIngestWorker) Timeout(*river.Job[DeferredIngestArgs]) time.Duration {
	return 5 * time.Minute
}
