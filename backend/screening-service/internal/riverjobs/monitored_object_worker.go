package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

type MonitoredObjectRunner interface {
	RunMonitoredObject(ctx context.Context, tenantID, monitoredObjectID string) error
}

type MonitoredObjectWorker struct {
	river.WorkerDefaults[MonitoredObjectArgs]

	runner MonitoredObjectRunner
}

func NewMonitoredObjectWorker(runner MonitoredObjectRunner) MonitoredObjectWorker {
	return MonitoredObjectWorker{runner: runner}
}

func (w *MonitoredObjectWorker) Work(ctx context.Context, job *river.Job[MonitoredObjectArgs]) error {
	return w.runner.RunMonitoredObject(ctx, job.Args.TenantID, job.Args.MonitoredObjectID)
}

func (w *MonitoredObjectWorker) Timeout(*river.Job[MonitoredObjectArgs]) time.Duration {
	return 5 * time.Minute
}
