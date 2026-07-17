package riverjobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type ScreeningEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, screeningID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, screeningID string, scheduledAt *time.Time) error
}

type DatasetJobEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, jobID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, jobID string, scheduledAt *time.Time) error
}

type MonitoredObjectEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, monitoredObjectID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, monitoredObjectID string, scheduledAt *time.Time) error
}

type NoopScreeningEnqueuer struct{}

func (NoopScreeningEnqueuer) Enqueue(context.Context, string, string, *time.Time) error { return nil }
func (NoopScreeningEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type NoopDatasetJobEnqueuer struct{}

func (NoopDatasetJobEnqueuer) Enqueue(context.Context, string, string, *time.Time) error { return nil }
func (NoopDatasetJobEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type NoopMonitoredObjectEnqueuer struct{}

func (NoopMonitoredObjectEnqueuer) Enqueue(context.Context, string, string, *time.Time) error {
	return nil
}
func (NoopMonitoredObjectEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type RiverScreeningEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

type RiverDatasetJobEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

type RiverMonitoredObjectEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverScreeningEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverScreeningEnqueuer {
	if queueName == "" {
		queueName = ScreeningQueueName
	}
	return RiverScreeningEnqueuer{
		client:      client,
		maxAttempts: max(1, maxAttempts),
		queueName:   queueName,
	}
}

func NewRiverDatasetJobEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverDatasetJobEnqueuer {
	if queueName == "" {
		queueName = DatasetJobQueueName
	}
	return RiverDatasetJobEnqueuer{
		client:      client,
		maxAttempts: max(1, maxAttempts),
		queueName:   queueName,
	}
}

func NewRiverMonitoredObjectEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverMonitoredObjectEnqueuer {
	if queueName == "" {
		queueName = MonitoredObjectQueueName
	}
	return RiverMonitoredObjectEnqueuer{
		client:      client,
		maxAttempts: max(1, maxAttempts),
		queueName:   queueName,
	}
}

func (e RiverScreeningEnqueuer) Enqueue(ctx context.Context, tenantID, screeningID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return e.enqueue(ctx, nil, tenantID, screeningID, scheduledAt)
}

func (e RiverScreeningEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, screeningID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return e.enqueue(ctx, tx, tenantID, screeningID, scheduledAt)
}

func (e RiverDatasetJobEnqueuer) Enqueue(ctx context.Context, tenantID, jobID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return e.enqueue(ctx, nil, tenantID, jobID, scheduledAt)
}

func (e RiverDatasetJobEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, jobID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return e.enqueue(ctx, tx, tenantID, jobID, scheduledAt)
}

func (e RiverMonitoredObjectEnqueuer) Enqueue(ctx context.Context, tenantID, monitoredObjectID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return e.enqueue(ctx, nil, tenantID, monitoredObjectID, scheduledAt)
}

func (e RiverMonitoredObjectEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, monitoredObjectID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return e.enqueue(ctx, tx, tenantID, monitoredObjectID, scheduledAt)
}

func (e RiverScreeningEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, tenantID, screeningID string, scheduledAt *time.Time) error {
	opts := &river.InsertOpts{
		MaxAttempts: e.maxAttempts,
		Queue:       e.queueName,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
	if scheduledAt != nil {
		opts.ScheduledAt = *scheduledAt
	}

	args := ScreeningArgs{
		TenantID:    tenantID,
		ScreeningID: screeningID,
	}
	if tx != nil {
		_, err := e.client.InsertTx(ctx, tx, args, opts)
		return err
	}
	_, err := e.client.Insert(ctx, args, opts)
	return err
}

func (e RiverDatasetJobEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, tenantID, jobID string, scheduledAt *time.Time) error {
	opts := &river.InsertOpts{
		MaxAttempts: e.maxAttempts,
		Queue:       e.queueName,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
	if scheduledAt != nil {
		opts.ScheduledAt = *scheduledAt
	}

	args := DatasetJobArgs{
		TenantID: tenantID,
		JobID:    jobID,
	}
	if tx != nil {
		_, err := e.client.InsertTx(ctx, tx, args, opts)
		return err
	}
	_, err := e.client.Insert(ctx, args, opts)
	return err
}

func (e RiverMonitoredObjectEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, tenantID, monitoredObjectID string, scheduledAt *time.Time) error {
	opts := &river.InsertOpts{
		MaxAttempts: e.maxAttempts,
		Queue:       e.queueName,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
	if scheduledAt != nil {
		opts.ScheduledAt = *scheduledAt
	}

	args := MonitoredObjectArgs{
		TenantID:          tenantID,
		MonitoredObjectID: monitoredObjectID,
	}
	if tx != nil {
		_, err := e.client.InsertTx(ctx, tx, args, opts)
		return err
	}
	_, err := e.client.Insert(ctx, args, opts)
	return err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
