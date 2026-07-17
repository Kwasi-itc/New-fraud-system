package riverjobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

type ScoringRequestRunner interface {
	RunScoringRequest(ctx context.Context, tenantID, requestID string) error
}

type ScoringRequestWorker struct {
	river.WorkerDefaults[ScoringRequestArgs]

	runner ScoringRequestRunner
}

func NewScoringRequestWorker(runner ScoringRequestRunner) ScoringRequestWorker {
	return ScoringRequestWorker{runner: runner}
}

func (w *ScoringRequestWorker) Work(ctx context.Context, job *river.Job[ScoringRequestArgs]) error {
	return w.runner.RunScoringRequest(ctx, job.Args.TenantID, job.Args.RequestID)
}

func (w *ScoringRequestWorker) Timeout(*river.Job[ScoringRequestArgs]) time.Duration {
	return 5 * time.Minute
}
