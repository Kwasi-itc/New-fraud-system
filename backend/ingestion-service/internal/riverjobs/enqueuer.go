package riverjobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type UploadLogEnqueuer interface {
	Enqueue(ctx context.Context, uploadLogID uuid.UUID, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, scheduledAt *time.Time) error
}

type NoopUploadLogEnqueuer struct{}

func (NoopUploadLogEnqueuer) Enqueue(context.Context, uuid.UUID, *time.Time) error { return nil }
func (NoopUploadLogEnqueuer) EnqueueTx(context.Context, pgx.Tx, uuid.UUID, *time.Time) error {
	return nil
}

type RiverUploadLogEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverUploadLogEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverUploadLogEnqueuer {
	if queueName == "" {
		queueName = UploadLogQueueName
	}
	return RiverUploadLogEnqueuer{
		client:      client,
		maxAttempts: maxAttempts,
		queueName:   queueName,
	}
}

func (e RiverUploadLogEnqueuer) Enqueue(ctx context.Context, uploadLogID uuid.UUID, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return e.enqueue(ctx, nil, uploadLogID, scheduledAt)
}

func (e RiverUploadLogEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return e.enqueue(ctx, tx, uploadLogID, scheduledAt)
}

func (e RiverUploadLogEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, scheduledAt *time.Time) error {
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
		_, err = e.client.InsertTx(ctx, tx, UploadLogArgs{UploadLogID: uploadLogID}, opts)
		return err
	}
	_, err = e.client.Insert(ctx, UploadLogArgs{UploadLogID: uploadLogID}, opts)
	return err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
