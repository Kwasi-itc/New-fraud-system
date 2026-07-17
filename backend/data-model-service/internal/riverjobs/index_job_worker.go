package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
)

type IndexJobWorker struct {
	river.WorkerDefaults[IndexJobArgs]

	runner worker.Runner
}

func NewIndexJobWorker(runner worker.Runner) IndexJobWorker {
	return IndexJobWorker{runner: runner}
}

func (w *IndexJobWorker) Work(ctx context.Context, job *river.Job[IndexJobArgs]) error {
	return w.runner.RunJob(ctx, job.Args.IndexJobID)
}

func (w *IndexJobWorker) Timeout(*river.Job[IndexJobArgs]) time.Duration {
	return 30 * time.Second
}
