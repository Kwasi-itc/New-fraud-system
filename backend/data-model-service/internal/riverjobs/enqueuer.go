package riverjobs

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type IndexJobEnqueuer interface {
	Enqueue(ctx context.Context, jobID uuid.UUID, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, scheduledAt *time.Time) error
}

type RiverIndexJobEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverIndexJobEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverIndexJobEnqueuer {
	if queueName == "" {
		queueName = IndexJobQueueName
	}
	return RiverIndexJobEnqueuer{
		client:      client,
		maxAttempts: maxAttempts,
		queueName:   queueName,
	}
}

func (e RiverIndexJobEnqueuer) Enqueue(ctx context.Context, jobID uuid.UUID, scheduledAt *time.Time) error {
	if e.client == nil {
		return ports.ErrJobQueueUnavailable
	}
	return e.enqueue(ctx, nil, jobID, scheduledAt)
}

func (e RiverIndexJobEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return ports.ErrJobQueueUnavailable
	}
	return e.enqueue(ctx, tx, jobID, scheduledAt)
}

func (e RiverIndexJobEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, scheduledAt *time.Time) error {
	opts := &river.InsertOpts{
		MaxAttempts: max(1, e.maxAttempts),
		Queue:       e.queueName,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
	if scheduledAt != nil {
		opts.ScheduledAt = *scheduledAt
	}
	var err error
	if tx != nil {
		_, err = e.client.InsertTx(ctx, tx, IndexJobArgs{IndexJobID: jobID}, opts)
		return err
	}
	_, err = e.client.Insert(ctx, IndexJobArgs{IndexJobID: jobID}, opts)
	return err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
