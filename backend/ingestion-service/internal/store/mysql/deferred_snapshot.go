package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrWorkMetricsIntegrity = errors.New("deferred metrics resource has no valid current job")

// DeferredMetricsSnapshot counts resources in this authorized tenant/table,
// never historical jobs or another table's work. A bounded indexed resource
// range joins each current job by primary key and transfers no payloads/keys.
// Overflow or inconsistent lineage returns no partial snapshot. The existing
// tenant-wide WorkInspector.DeferredMetrics remains an operator-only contract.
func (w *WorkOwner) DeferredMetricsSnapshot(ctx context.Context, limit int) (ingestion.DeferredIngestMetrics, error) {
	var empty ingestion.DeferredIngestMetrics
	if w == nil || limit < 1 || limit > 100000 {
		return empty, ErrWorkMetricsBound
	}
	scopes := []ReceiptRequest{w.scope(ingestion.ModeCreate, DeferredWork), w.scope(ingestion.ModePatch, DeferredWork)}
	for _, scope := range scopes {
		if err := w.owner.receipts.authorize(ctx, scope); err != nil {
			return empty, err
		}
	}
	tx, err := w.owner.receipts.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	for _, scope := range scopes {
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return empty, err
		}
	}
	var clock mysqlstore.DateTime
	if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&clock); err != nil {
		return empty, err
	}
	result := ingestion.DeferredIngestMetrics{SnapshotAt: clock.Time}
	cutoff := clock.Time.Add(-5 * time.Minute)
	query, args := w.deferredSnapshotQuery(limit + 1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
		if n > limit {
			return empty, ErrWorkMetricsBound
		}
		var requested mysqlstore.DateTime
		var state, kind, queue sql.NullString
		var attempt sql.NullInt64
		var started, finished mysqlstore.NullDateTime
		if err := rows.Scan(&requested, &state, &attempt, &started, &finished, &kind, &queue); err != nil {
			return empty, err
		}
		if !state.Valid || !attempt.Valid || attempt.Int64 < 0 || kind.String != DeferredWork || queue.String != w.deferredQueue {
			return empty, ErrWorkMetricsIntegrity
		}
		switch state.String {
		case "available":
			result.QueuedCount++
			if attempt.Int64 > 0 {
				result.RetryPendingCount++
			}
			if result.OldestQueuedRequestedAt == nil || requested.Time.Before(*result.OldestQueuedRequestedAt) {
				at := requested.Time
				result.OldestQueuedRequestedAt = &at
			}
		case "running":
			result.ProcessingCount++
		case "completed":
			result.CompletedCount++
			if finished.Valid && !finished.DateTime.Time.Before(cutoff) {
				result.RecentSuccessCount++
			}
		case "discarded", "cancelled":
			// Matches the existing resource GET DTO's terminal failed status.
			result.FailedCount++
			if finished.Valid && !finished.DateTime.Time.Before(cutoff) {
				result.RecentFailureCount++
			}
		default:
			return empty, ErrWorkMetricsIntegrity
		}
		if attempt.Int64 > 1 {
			result.RetriedExecutionCount++
			// Preserve PostgreSQL's execution-count semantics: this is not a
			// fabricated count of every historical retry attempt.
			if started.Valid && !started.DateTime.Time.Before(cutoff) {
				result.RecentRetryCount++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}
	if err := rows.Close(); err != nil {
		return empty, err
	}
	if result.OldestQueuedRequestedAt != nil {
		result.OldestQueuedAgeSeconds = max(0, clock.Time.Sub(*result.OldestQueuedRequestedAt).Seconds())
	}
	result.DrainRatePerMinuteLast5Min = float64(result.RecentSuccessCount) / 5
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return result, nil
}

func (w *WorkOwner) deferredSnapshotQuery(limit int) (string, []any) {
	s := w.owner.receipts.schema
	q := "SELECT r.created_at,j.state,j.attempt,j.started_at,COALESCE(r.completed_at,j.finished_at),j.kind,j.queue FROM " + s + ".ingestion_work_resources r FORCE INDEX (upload_history) LEFT JOIN " + s + ".ingestion_work_jobs j ON j.tenant_id=r.tenant_id AND j.resource_id=r.id AND j.id=COALESCE(r.current_job_id,r.id) WHERE r.tenant_id=? AND r.table_id=? AND r.kind=? ORDER BY r.created_at DESC,r.id DESC LIMIT ?"
	return q, []any{mysqlstore.UUID(w.owner.receipts.tenant), mysqlstore.UUID(w.owner.table), DeferredWork, limit}
}
