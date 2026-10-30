package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
)

var ErrWorkMetricsBound = errors.New("deferred metrics exceed exact count bound")

type DeferredWorkMetrics struct{ Pending, Running, Completed, Discarded, Cancelled int64 }

func (m DeferredWorkMetrics) Total() int64 {
	return m.Pending + m.Running + m.Completed + m.Discarded + m.Cancelled
}
func WorkMetricsIndex(database string) (string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	return "ALTER TABLE " + s + ".ingestion_work_jobs ADD INDEX deferred_metrics(tenant_id,queue,kind,state)", nil
}

// DeferredMetrics returns one consistent snapshot across all five states, after
// authorizing every scope. Each index range visits at most maxPerState+1 rows;
// overflow fails the entire result rather than misreporting a truncated count.
// Scheduled available work is included in Pending; no payload/error/key is read.
func (r *WorkInspector) DeferredMetrics(ctx context.Context, queue string, maxPerState int) (DeferredWorkMetrics, error) {
	var result DeferredWorkMetrics
	if r == nil || !work.ValidQueueName(queue) || maxPerState < 1 || maxPerState > 100000 {
		return result, ErrWorkMetricsBound
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	states := []string{"available", "running", "completed", "discarded", "cancelled"}
	for _, state := range states {
		if err = r.authorize(ctx, tx, QueueInspectionScope{r.tenant, queue, state}); err != nil {
			return DeferredWorkMetrics{}, err
		}
	}
	values := []*int64{&result.Pending, &result.Running, &result.Completed, &result.Discarded, &result.Cancelled}
	for i, state := range states {
		query := "SELECT COUNT(*) FROM (SELECT id FROM " + r.schema + ".ingestion_work_jobs FORCE INDEX(deferred_metrics) WHERE tenant_id=? AND queue=? AND kind=? AND state=? LIMIT ?) bounded"
		if err = tx.QueryRowContext(ctx, query, mysqlstore.UUID(r.tenant), queue, DeferredWork, state, maxPerState+1).Scan(values[i]); err != nil {
			return DeferredWorkMetrics{}, err
		}
		if *values[i] > int64(maxPerState) {
			return DeferredWorkMetrics{}, ErrWorkMetricsBound
		}
	}
	if err = tx.Commit(); err != nil {
		return DeferredWorkMetrics{}, err
	}
	return result, nil
}
