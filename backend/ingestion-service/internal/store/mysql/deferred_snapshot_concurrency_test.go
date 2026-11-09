package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestFullSharedDeferredSnapshotConcurrentCompletion(t *testing.T) {
	_, f, w, _ := metricFixture(t)
	original := w.owner.receipts.authorizeTx
	var once sync.Once
	w.owner.receipts.authorizeTx = func(ctx context.Context, tx *sql.Tx, scope ReceiptRequest) error {
		if err := original(ctx, tx, scope); err != nil {
			return err
		}
		// The ordinary metadata authorization read has established the RR
		// snapshot. A concurrent committed completion must not split its totals.
		once.Do(func() {
			fixtureExec(t, f.db, "UPDATE ingestion_work_jobs j JOIN ingestion_work_resources r ON r.tenant_id=j.tenant_id AND r.id=j.resource_id SET j.state='completed',j.finished_at=UTC_TIMESTAMP(6) WHERE r.tenant_id=? AND r.table_id=? AND r.kind=? AND j.state='available'", mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), DeferredWork)
		})
		return nil
	}
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	got, err := w.DeferredMetricsSnapshot(ctx, 8)
	if err != nil || got.QueuedCount != 2 || got.CompletedCount != 2 {
		t.Fatal("mixed transaction snapshots", got, err)
	}
	w.owner.receipts.authorizeTx = original
	got, err = w.DeferredMetricsSnapshot(ctx, 8)
	if err != nil || got.QueuedCount != 0 || got.CompletedCount != 4 || got.OldestQueuedRequestedAt != nil {
		t.Fatal("new snapshot did not observe completion", got, err)
	}
}

func TestDeferredSnapshotIndexedHistory(t *testing.T) {
	f, w := workFixture(t)
	seedUploadList(t, f, w, 2000)
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET queue='deferred' WHERE kind=?", DeferredWork)
	got, err := w.DeferredMetricsSnapshot(context.Background(), 1000)
	if err != nil || got.CompletedCount != 1000 || got.RecentSuccessCount != 0 {
		t.Fatal(got, err)
	}
	if _, err := w.DeferredMetricsSnapshot(context.Background(), 999); !errors.Is(err, ErrWorkMetricsBound) {
		t.Fatal("history limit not enforced", err)
	}
	query, args := w.deferredSnapshotQuery(1001)
	var plan string
	if err := f.db.QueryRow("EXPLAIN FORMAT=JSON "+query, args...).Scan(&plan); err != nil || !strings.Contains(plan, `"key": "upload_history"`) || !strings.Contains(plan, `"key": "PRIMARY"`) || strings.Contains(plan, `"using_filesort": true`) {
		t.Fatal("unbounded or sorted history plan", plan, err)
	}
	t.Log("2,000-resource mixed history plan:", plan)
}
