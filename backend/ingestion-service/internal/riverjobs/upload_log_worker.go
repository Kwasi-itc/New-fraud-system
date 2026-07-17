package riverjobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

type UploadLogRunner interface {
	RunLog(ctx context.Context, id uuid.UUID) error
}

type UploadLogWorker struct {
	river.WorkerDefaults[UploadLogArgs]

	runner UploadLogRunner
}

func NewUploadLogWorker(runner UploadLogRunner) UploadLogWorker {
	return UploadLogWorker{runner: runner}
}

func (w *UploadLogWorker) Work(ctx context.Context, job *river.Job[UploadLogArgs]) error {
	return w.runner.RunLog(ctx, job.Args.UploadLogID)
}

func (w *UploadLogWorker) Timeout(*river.Job[UploadLogArgs]) time.Duration {
	return 5 * time.Minute
}
