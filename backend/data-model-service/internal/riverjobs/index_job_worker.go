package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
)

type IndexJobWorker struct {
	river.WorkerDefaults[IndexJobArgs]

	runner  worker.Runner
	timeout time.Duration
}

func NewIndexJobWorker(runner worker.Runner, timeout ...time.Duration) IndexJobWorker {
	value := 2 * time.Hour
	if len(timeout) > 0 && timeout[0] > 0 {
		value = timeout[0]
	}
	return IndexJobWorker{runner: runner, timeout: value}
}

func (w *IndexJobWorker) Work(ctx context.Context, job *river.Job[IndexJobArgs]) error {
	return w.runner.RunJob(ctx, job.Args.IndexJobID)
}

func (w *IndexJobWorker) Timeout(*river.Job[IndexJobArgs]) time.Duration {
	return w.timeout
}
