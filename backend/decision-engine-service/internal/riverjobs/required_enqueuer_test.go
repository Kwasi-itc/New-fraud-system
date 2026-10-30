package riverjobs

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func TestEveryRequiredProducerRejectsMissingInfrastructure(t *testing.T) {
	ctx := context.Background()
	resourceOnly := []interface {
		Enqueue(context.Context, string, *time.Time) error
		EnqueueTx(context.Context, pgx.Tx, string, *time.Time) error
	}{NewRiverScheduledExecutionEnqueuer(nil, 3, ""), NewRiverAsyncDecisionExecutionEnqueuer(nil, 3, "")}
	for _, e := range resourceOnly {
		if e.Enqueue(ctx, "resource", nil) == nil || e.EnqueueTx(ctx, nil, "resource", nil) == nil {
			t.Fatal("required producer silently succeeded")
		}
	}
	scoped := []interface {
		Enqueue(context.Context, string, string, *time.Time) error
		EnqueueTx(context.Context, pgx.Tx, string, string, *time.Time) error
	}{NewRiverAsyncDecisionExecutionCallbackEnqueuer(nil, 3, ""), NewRiverWorkflowExecutionEnqueuer(nil, 1, ""), NewRiverScreeningExecutionEnqueuer(nil, 1, ""), NewRiverScoringRequestEnqueuer(nil, 1, ""), NewRiverOutboxEventEnqueuer(nil, 1, "")}
	for _, e := range scoped {
		if e.Enqueue(ctx, "tenant", "resource", nil) == nil || e.EnqueueTx(ctx, nil, "tenant", "resource", nil) == nil {
			t.Fatal("required scoped producer silently succeeded")
		}
	}
}
