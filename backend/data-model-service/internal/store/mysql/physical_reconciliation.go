package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PhysicalRecoveryResult describes execution progress, never publication or
// readiness. Failure codes are safe for operator reports; the returned error
// retains causes for trusted diagnostics. Plans, SQL and actor data are omitted.
type PhysicalRecoveryResult struct {
	TableID     uuid.UUID `json:"table_id"`
	OperationID uuid.UUID `json:"operation_id"`
	Outcome     string    `json:"outcome"`
}

type PhysicalRecoveryPage struct {
	Results []PhysicalRecoveryResult `json:"results"`
	After   uuid.UUID                `json:"after"`
	// Exhausted describes this state-filtered page snapshot only. New intents
	// and state transitions require a subsequent sweep starting at the zero UUID.
	Exhausted bool `json:"exhausted"`
}

type physicalRecoveryItem struct{ table, operation uuid.UUID }

// ReconcilePage resumes one bounded tenant/state page, sequentially, using the
// authoritative immutable intents and the existing pinned-connection executor.
// It never creates intents, force-cleans journals, opens gates or publishes.
// The caller must authorize this tenant and budget the DDL pool. Blocked work
// requires an explicit state selection; no automatic retry loop is introduced.
func (e *PhysicalExecutor) ReconcilePage(ctx context.Context, state string, after uuid.UUID, limit int, operationTimeout time.Duration) (PhysicalRecoveryPage, error) {
	page := PhysicalRecoveryPage{After: after, Results: []PhysicalRecoveryResult{}}
	switch state {
	case "prepared", "applying", "verifying", "blocked":
	default:
		return page, errors.New("pending intent state required")
	}
	if limit < 1 || limit > 1000 || operationTimeout <= 0 {
		return page, errors.New("page limit 1..1000 and positive operation timeout required")
	}
	// Project the routing identity in the same indexed query. Do not load each
	// potentially large plan merely to discover its table. Close rows before DDL
	// so a single-connection pool works without retaining a read transaction.
	rows, err := e.db.QueryContext(ctx, "SELECT table_id,operation_id FROM "+e.schema+".data_model_schema_change_intents WHERE tenant_id=? AND state=? AND operation_id>? ORDER BY operation_id LIMIT ?", mysqlstore.UUID(e.tenant), state, mysqlstore.UUID(after), limit+1)
	if err != nil {
		return page, err
	}
	items := make([]physicalRecoveryItem, 0, limit+1)
	for rows.Next() {
		var item physicalRecoveryItem
		if err = rows.Scan((*mysqlstore.UUID)(&item.table), (*mysqlstore.UUID)(&item.operation)); err != nil {
			break
		}
		items = append(items, item)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return page, err
	}
	page.Exhausted = len(items) <= limit
	if len(items) > limit {
		items = items[:limit]
	}
	return recoverPhysicalItems(ctx, page, items, operationTimeout, e.ExecuteSteps)
}

func recoverPhysicalItems(ctx context.Context, page PhysicalRecoveryPage, items []physicalRecoveryItem, timeout time.Duration, execute func(context.Context, uuid.UUID, uuid.UUID) error) (PhysicalRecoveryPage, error) {
	var failures []error
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			page.Exhausted = false
			return page, errors.Join(append(failures, err)...)
		}
		operationCtx, cancel := context.WithTimeout(ctx, timeout)
		err := execute(operationCtx, item.table, item.operation)
		cancel()
		outcome := "awaiting_publication"
		if err != nil {
			outcome = physicalRecoveryCode(err)
			failures = append(failures, fmt.Errorf("physical operation %s: %w", item.operation, err))
		}
		page.Results = append(page.Results, PhysicalRecoveryResult{item.table, item.operation, outcome})
		page.After = item.operation
	}
	return page, errors.Join(failures...)
}

func physicalRecoveryCode(err error) string {
	// An unconfirmed cleanup/commit takes precedence over a secondary drift or
	// cancellation error. Operators must inspect uncertain outcomes before retry.
	var cleanup *mysqlstore.DDLLockCleanupError
	var commit *mysqlstore.CommitError
	if errors.As(err, &cleanup) || errors.As(err, &commit) {
		return "outcome_uncertain"
	}
	switch {
	case errors.Is(err, mysqlstore.ErrDDLLockBusy):
		return "lock_busy"
	case errors.Is(err, migrations.ErrDrift):
		return "schema_drift"
	case errors.Is(err, migrations.ErrLineage):
		return "journal_conflict"
	case errors.Is(err, ErrPhysicalPrecondition):
		return "data_precondition"
	case errors.Is(err, ErrPhysicalConflict):
		return "operation_conflict"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "execution_failed"
	}
}
