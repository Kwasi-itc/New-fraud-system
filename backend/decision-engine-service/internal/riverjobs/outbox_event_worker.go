package riverjobs

import (
	"context"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"time"

	"github.com/riverqueue/river"
)

type OutboxEventRunner interface {
	RunOutboxEvent(ctx context.Context, tenantID, eventID string) error
}

type OutboxEventWorker struct {
	river.WorkerDefaults[OutboxEventArgs]

	runner OutboxEventRunner
}

func NewOutboxEventWorker(runner OutboxEventRunner) OutboxEventWorker {
	return OutboxEventWorker{runner: runner}
}

func (w *OutboxEventWorker) Work(ctx context.Context, job *river.Job[OutboxEventArgs]) error {
	ctx = platformauth.WithOrigin(ctx, job.Args.Origin)
	return w.runner.RunOutboxEvent(ctx, job.Args.TenantID, job.Args.EventID)
}

func (w *OutboxEventWorker) Timeout(*river.Job[OutboxEventArgs]) time.Duration {
	return 5 * time.Minute
}
