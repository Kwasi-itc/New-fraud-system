package riverjobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type AsyncDecisionExecutionEnqueuer interface {
	Enqueue(ctx context.Context, executionID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, executionID string, scheduledAt *time.Time) error
}

type AsyncDecisionExecutionCallbackEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, executionID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, executionID string, scheduledAt *time.Time) error
}

type ScheduledExecutionEnqueuer interface {
	Enqueue(ctx context.Context, executionID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, executionID string, scheduledAt *time.Time) error
}

type WorkflowExecutionEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, executionID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, executionID string, scheduledAt *time.Time) error
}

type ScreeningExecutionEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, executionID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, executionID string, scheduledAt *time.Time) error
}

type ScoringRequestEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, requestID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, requestID string, scheduledAt *time.Time) error
}

type OutboxEventEnqueuer interface {
	Enqueue(ctx context.Context, tenantID, eventID string, scheduledAt *time.Time) error
	EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, eventID string, scheduledAt *time.Time) error
}

type NoopAsyncDecisionExecutionEnqueuer struct{}

func (NoopAsyncDecisionExecutionEnqueuer) Enqueue(context.Context, string, *time.Time) error {
	return nil
}
func (NoopAsyncDecisionExecutionEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, *time.Time) error {
	return nil
}

type NoopAsyncDecisionExecutionCallbackEnqueuer struct{}

func (NoopAsyncDecisionExecutionCallbackEnqueuer) Enqueue(context.Context, string, string, *time.Time) error {
	return nil
}
func (NoopAsyncDecisionExecutionCallbackEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type NoopScheduledExecutionEnqueuer struct{}

func (NoopScheduledExecutionEnqueuer) Enqueue(context.Context, string, *time.Time) error { return nil }
func (NoopScheduledExecutionEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, *time.Time) error {
	return nil
}

type NoopWorkflowExecutionEnqueuer struct{}

func (NoopWorkflowExecutionEnqueuer) Enqueue(context.Context, string, string, *time.Time) error {
	return nil
}
func (NoopWorkflowExecutionEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type NoopScreeningExecutionEnqueuer struct{}

func (NoopScreeningExecutionEnqueuer) Enqueue(context.Context, string, string, *time.Time) error {
	return nil
}
func (NoopScreeningExecutionEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type NoopScoringRequestEnqueuer struct{}

func (NoopScoringRequestEnqueuer) Enqueue(context.Context, string, string, *time.Time) error {
	return nil
}
func (NoopScoringRequestEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type NoopOutboxEventEnqueuer struct{}

func (NoopOutboxEventEnqueuer) Enqueue(context.Context, string, string, *time.Time) error { return nil }
func (NoopOutboxEventEnqueuer) EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error {
	return nil
}

type RiverAsyncDecisionExecutionEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

type RiverAsyncDecisionExecutionCallbackEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverAsyncDecisionExecutionCallbackEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverAsyncDecisionExecutionCallbackEnqueuer {
	if queueName == "" {
		queueName = AsyncDecisionExecutionCallbackQueueName
	}
	return RiverAsyncDecisionExecutionCallbackEnqueuer{client: client, maxAttempts: maxAttempts, queueName: queueName}
}

func (e RiverAsyncDecisionExecutionCallbackEnqueuer) Enqueue(ctx context.Context, tenantID, executionID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return enqueue(ctx, e.client, nil, AsyncDecisionExecutionCallbackArgs{TenantID: tenantID, ExecutionID: executionID}, e.maxAttempts, e.queueName, scheduledAt)
}

func (e RiverAsyncDecisionExecutionCallbackEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, executionID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return enqueue(ctx, e.client, tx, AsyncDecisionExecutionCallbackArgs{TenantID: tenantID, ExecutionID: executionID}, e.maxAttempts, e.queueName, scheduledAt)
}

func NewRiverAsyncDecisionExecutionEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverAsyncDecisionExecutionEnqueuer {
	if queueName == "" {
		queueName = AsyncDecisionExecutionQueueName
	}
	return RiverAsyncDecisionExecutionEnqueuer{
		client:      client,
		maxAttempts: maxAttempts,
		queueName:   queueName,
	}
}

func (e RiverAsyncDecisionExecutionEnqueuer) Enqueue(ctx context.Context, executionID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return e.enqueue(ctx, nil, executionID, scheduledAt)
}

func (e RiverAsyncDecisionExecutionEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, executionID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return e.enqueue(ctx, tx, executionID, scheduledAt)
}

func (e RiverAsyncDecisionExecutionEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, executionID string, scheduledAt *time.Time) error {
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
		_, err = e.client.InsertTx(ctx, tx, AsyncDecisionExecutionArgs{ExecutionID: executionID}, opts)
		return err
	}
	_, err = e.client.Insert(ctx, AsyncDecisionExecutionArgs{ExecutionID: executionID}, opts)
	return err
}

type RiverScheduledExecutionEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverScheduledExecutionEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverScheduledExecutionEnqueuer {
	if queueName == "" {
		queueName = ScheduledExecutionQueueName
	}
	return RiverScheduledExecutionEnqueuer{
		client:      client,
		maxAttempts: maxAttempts,
		queueName:   queueName,
	}
}

func (e RiverScheduledExecutionEnqueuer) Enqueue(ctx context.Context, executionID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return e.enqueue(ctx, nil, executionID, scheduledAt)
}

func (e RiverScheduledExecutionEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, executionID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return e.enqueue(ctx, tx, executionID, scheduledAt)
}

func (e RiverScheduledExecutionEnqueuer) enqueue(ctx context.Context, tx pgx.Tx, executionID string, scheduledAt *time.Time) error {
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
		_, err = e.client.InsertTx(ctx, tx, ScheduledExecutionArgs{ExecutionID: executionID}, opts)
		return err
	}
	_, err = e.client.Insert(ctx, ScheduledExecutionArgs{ExecutionID: executionID}, opts)
	return err
}

type RiverWorkflowExecutionEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverWorkflowExecutionEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverWorkflowExecutionEnqueuer {
	if queueName == "" {
		queueName = WorkflowExecutionQueueName
	}
	return RiverWorkflowExecutionEnqueuer{client: client, maxAttempts: maxAttempts, queueName: queueName}
}

func (e RiverWorkflowExecutionEnqueuer) Enqueue(ctx context.Context, tenantID, executionID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return enqueue(ctx, e.client, nil, WorkflowExecutionArgs{TenantID: tenantID, ExecutionID: executionID}, e.maxAttempts, e.queueName, scheduledAt)
}

func (e RiverWorkflowExecutionEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, executionID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return enqueue(ctx, e.client, tx, WorkflowExecutionArgs{TenantID: tenantID, ExecutionID: executionID}, e.maxAttempts, e.queueName, scheduledAt)
}

type RiverScreeningExecutionEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverScreeningExecutionEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverScreeningExecutionEnqueuer {
	if queueName == "" {
		queueName = ScreeningExecutionQueueName
	}
	return RiverScreeningExecutionEnqueuer{client: client, maxAttempts: maxAttempts, queueName: queueName}
}

func (e RiverScreeningExecutionEnqueuer) Enqueue(ctx context.Context, tenantID, executionID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return enqueue(ctx, e.client, nil, ScreeningExecutionArgs{TenantID: tenantID, ExecutionID: executionID}, e.maxAttempts, e.queueName, scheduledAt)
}

func (e RiverScreeningExecutionEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, executionID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return enqueue(ctx, e.client, tx, ScreeningExecutionArgs{TenantID: tenantID, ExecutionID: executionID}, e.maxAttempts, e.queueName, scheduledAt)
}

type RiverScoringRequestEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverScoringRequestEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverScoringRequestEnqueuer {
	if queueName == "" {
		queueName = ScoringRequestQueueName
	}
	return RiverScoringRequestEnqueuer{client: client, maxAttempts: maxAttempts, queueName: queueName}
}

func (e RiverScoringRequestEnqueuer) Enqueue(ctx context.Context, tenantID, requestID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return enqueue(ctx, e.client, nil, ScoringRequestArgs{TenantID: tenantID, RequestID: requestID}, e.maxAttempts, e.queueName, scheduledAt)
}

func (e RiverScoringRequestEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, requestID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return enqueue(ctx, e.client, tx, ScoringRequestArgs{TenantID: tenantID, RequestID: requestID}, e.maxAttempts, e.queueName, scheduledAt)
}

type RiverOutboxEventEnqueuer struct {
	client      *river.Client[pgx.Tx]
	maxAttempts int
	queueName   string
}

func NewRiverOutboxEventEnqueuer(client *river.Client[pgx.Tx], maxAttempts int, queueName string) RiverOutboxEventEnqueuer {
	if queueName == "" {
		queueName = OutboxEventQueueName
	}
	return RiverOutboxEventEnqueuer{client: client, maxAttempts: maxAttempts, queueName: queueName}
}

func (e RiverOutboxEventEnqueuer) Enqueue(ctx context.Context, tenantID, eventID string, scheduledAt *time.Time) error {
	if e.client == nil {
		return nil
	}
	return enqueue(ctx, e.client, nil, OutboxEventArgs{TenantID: tenantID, EventID: eventID}, e.maxAttempts, e.queueName, scheduledAt)
}

func (e RiverOutboxEventEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, tenantID, eventID string, scheduledAt *time.Time) error {
	if e.client == nil || tx == nil {
		return nil
	}
	return enqueue(ctx, e.client, tx, OutboxEventArgs{TenantID: tenantID, EventID: eventID}, e.maxAttempts, e.queueName, scheduledAt)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func enqueue[T river.JobArgs](ctx context.Context, client *river.Client[pgx.Tx], tx pgx.Tx, args T, maxAttempts int, queueName string, scheduledAt *time.Time) error {
	opts := &river.InsertOpts{
		MaxAttempts: max(1, maxAttempts),
		Queue:       queueName,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
	if scheduledAt != nil {
		opts.ScheduledAt = *scheduledAt
	}
	var err error
	if tx != nil {
		_, err = client.InsertTx(ctx, tx, args, opts)
		return err
	}
	_, err = client.Insert(ctx, args, opts)
	return err
}
