package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

type DatasetJobRunner interface {
	RunJob(ctx context.Context, tenantID, jobID string) error
}

type DatasetJobWorker struct {
	river.WorkerDefaults[DatasetJobArgs]

	runner DatasetJobRunner
}

func NewDatasetJobWorker(runner DatasetJobRunner) DatasetJobWorker {
	return DatasetJobWorker{runner: runner}
}

func (w *DatasetJobWorker) Work(ctx context.Context, job *river.Job[DatasetJobArgs]) error {
	return w.runner.RunJob(ctx, job.Args.TenantID, job.Args.JobID)
}

func (w *DatasetJobWorker) Timeout(*river.Job[DatasetJobArgs]) time.Duration {
	return 5 * time.Minute
}
