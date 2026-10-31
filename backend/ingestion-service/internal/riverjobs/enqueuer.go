package riverjobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type UploadLogEnqueuer interface {
	Enqueue(ctx context.Context, uploadLogID uuid.UUID, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, scheduledAt *time.Time) error
	RetryTx(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, priorAttempt int, scheduledAt *time.Time) error
}

type DeferredIngestEnqueuer interface {
	Enqueue(ctx context.Context, deferredIngestID uuid.UUID, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, deferredIngestID uuid.UUID, scheduledAt *time.Time) error
	RetryTx(ctx context.Context, tx pgx.Tx, deferredIngestID uuid.UUID, priorAttempt int, scheduledAt *time.Time) error
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
		return errors.New("upload River client is not initialized")
	}
	return e.enqueue(ctx, nil, uploadLogID, scheduledAt)
}

func (e RiverUploadLogEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return errors.New("upload enqueue requires an initialized River client and transaction")
	}
	return e.enqueue(ctx, tx, uploadLogID, scheduledAt)
}

func (e RiverUploadLogEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, scheduledAt *time.Time) error {
	return e.insert(ctx, tx, uploadLogID, scheduledAt, 0)
}

// RetryTx uses a versioned per-domain-attempt identity. It preserves River's
// required unique states while preventing the running/completed prior attempt
// from swallowing a requested retry. Repeated requests for this attempt dedupe.
func (e RiverUploadLogEnqueuer) RetryTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, priorAttempt int, at *time.Time) error {
	if e.client == nil || tx == nil {
		return errors.New("upload retry requires an initialized River client and transaction")
	}
	if priorAttempt < 1 {
		return errors.New("positive prior upload attempt required")
	}
	return e.insert(ctx, tx, id, at, priorAttempt)
}

func (e RiverUploadLogEnqueuer) insert(ctx context.Context, tx pgx.Tx, uploadLogID uuid.UUID, scheduledAt *time.Time, priorAttempt int) error {
	if err := validateDeliveryPayload(uploadLogID, 1, priorAttempt); err != nil {
		return err
	}
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
		_, err = e.client.InsertTx(ctx, tx, UploadLogArgs{UploadLogID: uploadLogID, PayloadVersion: 1, RetryAttempt: priorAttempt}, opts)
		return err
	}
	_, err = e.client.Insert(ctx, UploadLogArgs{UploadLogID: uploadLogID, PayloadVersion: 1, RetryAttempt: priorAttempt}, opts)
	return err
}

type RiverDeferredIngestEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverDeferredIngestEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverDeferredIngestEnqueuer {
	if queueName == "" {
		queueName = DeferredIngestQueueName
	}
	return RiverDeferredIngestEnqueuer{
		client:      client,
		maxAttempts: maxAttempts,
		queueName:   queueName,
	}
}

func (e RiverDeferredIngestEnqueuer) Enqueue(ctx context.Context, deferredIngestID uuid.UUID, scheduledAt *time.Time) error {
	if e.client == nil {
		return errors.New("deferred River client is not initialized")
	}
	return e.enqueue(ctx, nil, deferredIngestID, scheduledAt)
}

func (e RiverDeferredIngestEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, deferredIngestID uuid.UUID, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return errors.New("deferred enqueue requires an initialized River client and transaction")
	}
	return e.enqueue(ctx, tx, deferredIngestID, scheduledAt)
}

func (e RiverDeferredIngestEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, deferredIngestID uuid.UUID, scheduledAt *time.Time) error {
	return e.insert(ctx, tx, deferredIngestID, scheduledAt, 0)
}

func (e RiverDeferredIngestEnqueuer) RetryTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, priorAttempt int, at *time.Time) error {
	if e.client == nil || tx == nil {
		return errors.New("deferred retry requires an initialized River client and transaction")
	}
	if priorAttempt < 1 {
		return errors.New("positive prior deferred attempt required")
	}
	return e.insert(ctx, tx, id, at, priorAttempt)
}

func (e RiverDeferredIngestEnqueuer) insert(ctx context.Context, tx pgx.Tx, deferredIngestID uuid.UUID, scheduledAt *time.Time, priorAttempt int) error {
	if err := validateDeliveryPayload(deferredIngestID, 1, priorAttempt); err != nil {
		return err
	}
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
		_, err = e.client.InsertTx(ctx, tx, DeferredIngestArgs{DeferredIngestID: deferredIngestID, PayloadVersion: 1, RetryAttempt: priorAttempt}, opts)
		return err
	}
	_, err = e.client.Insert(ctx, DeferredIngestArgs{DeferredIngestID: deferredIngestID, PayloadVersion: 1, RetryAttempt: priorAttempt}, opts)
	return err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
