package mysql

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// Pending domain rows are the native durable queue. Service writes and queue
// visibility therefore share one commit. Scheduled River jobs are not imported.
type DomainPollingEnqueuer struct{}

func (DomainPollingEnqueuer) Enqueue(ctx context.Context, t, key string, at *time.Time) error {
	return validatePoll(ctx, t, key, at)
}
func (DomainPollingEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, t, key string, at *time.Time) error {
	if tx != nil {
		return errors.New("native polling cannot use a PostgreSQL transaction")
	}
	return validatePoll(ctx, t, key, at)
}
func validatePoll(ctx context.Context, t, key string, at *time.Time) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	tenant, e1 := uuid.Parse(t)
	resource, e2 := uuid.Parse(key)
	if e1 != nil || e2 != nil || tenant == uuid.Nil || resource == uuid.Nil || at != nil {
		return errors.New("native polling requires current UUID work without a scheduled override")
	}
	return nil
}
