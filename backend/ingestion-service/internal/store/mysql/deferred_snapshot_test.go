package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type metricFixtureRow struct {
	state             string
	attempt           int
	requested         time.Time
	started, finished *time.Time
}

func seedMetricRow(t *testing.T, db *sql.DB, w *WorkOwner, kind string, row metricFixtureRow) uuid.UUID {
	t.Helper()
	input := workInput(kind)
	// Future scheduling is still queued, aged from the original request time.
	input.ScheduledAt = row.requested.Add(24 * time.Hour)
	if _, err := w.Create(WithServiceCredential(context.Background(), "fixture-token"), input); err != nil {
		t.Fatal(err)
	}
	fixtureExec(t, db, "UPDATE ingestion_work_resources SET created_at=?,completed_at=? WHERE id=?", row.requested, row.finished, mysqlstore.UUID(input.ID))
	fixtureExec(t, db, "UPDATE ingestion_work_jobs SET state=?,attempt=?,started_at=?,finished_at=?,lease_token=IF(?='running',UUID_TO_BIN(UUID()),NULL),lease_until=IF(?='running',UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE,NULL) WHERE id=?", row.state, row.attempt, row.started, row.finished, row.state, row.state, mysqlstore.UUID(input.ID))
	if row.state != "available" && row.state != "running" {
		fixtureExec(t, db, "DELETE FROM ingestion_work_reservations WHERE resource_id=?", mysqlstore.UUID(input.ID))
	}
	return input.ID
}

func metricFixture(t *testing.T) (sharedFixture, recordFixtureState, *WorkOwner, []metricFixtureRow) {
	t.Helper()
	shared, f := fullRecordFixture(t)
	owner, _ := fullOwner(t, shared, f, "runtime")
	w, err := NewWorkOwner(owner, "uploads", "deferred", 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	var clock mysqlstore.DateTime
	if err := f.db.QueryRow("SELECT UTC_TIMESTAMP(6)").Scan(&clock); err != nil {
		t.Fatal(err)
	}
	now := clock.Time
	ago := func(minutes int) *time.Time { at := now.Add(-time.Duration(minutes) * time.Minute); return &at }
	rows := []metricFixtureRow{
		{"available", 0, *ago(10), nil, nil},
		{"available", 1, *ago(2), ago(1), nil},
		{"running", 2, *ago(8), ago(1), nil},
		{"completed", 1, *ago(9), ago(2), ago(1)},
		{"completed", 2, *ago(20), ago(10), ago(10)},
		{"discarded", 3, *ago(8), ago(2), ago(1)},
		{"cancelled", 0, *ago(7), nil, ago(1)},
		{"discarded", 1, *ago(20), ago(11), ago(10)},
	}
	for _, row := range rows {
		seedMetricRow(t, f.db, w, DeferredWork, row)
	}
	seedMetricRow(t, f.db, w, UploadWork, metricFixtureRow{"completed", 2, now, &now, &now})
	return shared, f, w, rows
}

func TestFullSharedDeferredSnapshot(t *testing.T) {
	shared, f, w, _ := metricFixture(t)
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	got, err := w.DeferredMetricsSnapshot(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if got.QueuedCount != 2 || got.ProcessingCount != 1 || got.CompletedCount != 2 || got.FailedCount != 3 || got.RetryPendingCount != 1 || got.RetriedExecutionCount != 3 || got.RecentSuccessCount != 1 || got.RecentFailureCount != 2 || got.RecentRetryCount != 2 || got.DrainRatePerMinuteLast5Min != .2 || got.OldestQueuedRequestedAt == nil || got.OldestQueuedAgeSeconds < 600 {
		t.Fatalf("snapshot: %+v", got)
	}
	if math.Abs(got.SnapshotAt.Sub(*got.OldestQueuedRequestedAt).Seconds()-got.OldestQueuedAgeSeconds) > 0.000001 {
		t.Fatal("inconsistent database-clock age", got)
	}
	if v, err := w.DeferredMetricsSnapshot(ctx, 7); !errors.Is(err, ErrWorkMetricsBound) || !reflect.DeepEqual(v, ingestion.DeferredIngestMetrics{}) {
		t.Fatal("partial overflow snapshot", v, err)
	}
	if _, err := w.DeferredMetricsSnapshot(context.Background(), 8); !errors.Is(err, ErrOwnerAuthorization) {
		t.Fatal("missing credential accepted", err)
	}
	fixtureExec(t, f.db, "UPDATE data_model_tenants SET status='pending' WHERE id=?", mysqlstore.UUID(f.model.TenantID))
	if _, err := w.DeferredMetricsSnapshot(ctx, 8); !errors.Is(err, ErrOwnerAuthorization) {
		t.Fatal("revoked tenant accepted", err)
	}
	fixtureExec(t, f.db, "UPDATE data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(f.model.TenantID))
	fixtureExec(t, f.db, "UPDATE data_model_tables SET archived=TRUE WHERE id=?", mysqlstore.UUID(w.owner.table))
	if _, err := w.DeferredMetricsSnapshot(ctx, 8); !errors.Is(err, ErrOwnerAuthorization) {
		t.Fatal("archived table accepted", err)
	}
	fixtureExec(t, f.db, "UPDATE data_model_tables SET archived=FALSE WHERE id=?", mysqlstore.UUID(w.owner.table))
	query, args := w.deferredSnapshotQuery(9)
	var plan string
	if err := f.db.QueryRow("EXPLAIN FORMAT=JSON "+query, args...).Scan(&plan); err != nil || !strings.Contains(plan, `"key": "upload_history"`) || !strings.Contains(plan, `"key": "PRIMARY"`) {
		t.Fatal("bounded scope/current-job plan", plan, err)
	}
	t.Log("snapshot indexed resource range and exact job join:", plan)
	foreignTenant, foreignTable, _ := seedFullContract(t, shared)
	foreign, err := NewAuthorizedRecordOwner(f.db, shared.Database, foreignTenant, foreignTable, "transactions", "fixture-token", w.owner.limits, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	foreignWork, err := NewWorkOwner(foreign, "uploads", "deferred", 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	seedMetricRow(t, f.db, foreignWork, DeferredWork, metricFixtureRow{"available", 0, got.SnapshotAt, nil, nil})
	// A second table in the SAME tenant must also remain outside this scope.
	otherTable := uuid.New()
	fixtureExec(t, f.db, "INSERT INTO data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES(?,?,'other',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(otherTable), mysqlstore.UUID(w.owner.receipts.tenant))
	fixtureExec(t, f.db, "INSERT INTO data_model_ingestion_contracts(table_id,tenant_id,table_name,version,payload) VALUES(?,?,'other',UUID_TO_BIN(UUID()),JSON_OBJECT('id',?,'name','other','fields',JSON_OBJECT()))", mysqlstore.UUID(otherTable), mysqlstore.UUID(w.owner.receipts.tenant), otherTable.String())
	other, err := NewAuthorizedRecordOwner(f.db, shared.Database, w.owner.receipts.tenant, otherTable, "other", "fixture-token", w.owner.limits, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	otherWork, err := NewWorkOwner(other, "uploads", "deferred", 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	seedMetricRow(t, f.db, otherWork, DeferredWork, metricFixtureRow{"available", 0, got.SnapshotAt, nil, nil})
	if result, err := w.DeferredMetricsSnapshot(ctx, 8); err != nil || result.QueuedCount != 2 {
		t.Fatal("foreign resource affected snapshot/bound", result, err)
	}
	var login string
	if err := w.owner.receipts.db.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&login); err != nil {
		t.Fatal(err)
	}
	fixtureExec(t, f.db, "REVOKE SELECT ON "+shared.Database+".ingestion_work_resources FROM '"+login+"'@'%'")
	if result, err := w.DeferredMetricsSnapshot(ctx, 8); err == nil || !reflect.DeepEqual(result, ingestion.DeferredIngestMetrics{}) {
		t.Fatal("missing grant returned success", result, err)
	}
}

func TestFullSharedDeferredSnapshotRedriveAndIntegrity(t *testing.T) {
	_, f := fullRecordFixture(t)
	owner, err := NewAuthorizedRecordOwner(f.db, f.database, f.model.TenantID, f.model.Tables["records"].ID, "records", "fixture-token", RecordOwnerLimits{Receipt: ReceiptLimits{1024, 1 << 20, 16}, CommandBytes: 1 << 20, WriteBytes: 1 << 20, EffectRowBytes: 1 << 20, EffectBatchBytes: 4 << 20}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWorkOwner(owner, "uploads", "deferred", 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	empty, err := w.DeferredMetricsSnapshot(ctx, 1)
	if err != nil || empty.CompletedCount != 0 || empty.OldestQueuedRequestedAt != nil || empty.OldestQueuedAgeSeconds != 0 || empty.SnapshotAt.IsZero() {
		t.Fatal(empty, err)
	}
	now := empty.SnapshotAt
	id := seedMetricRow(t, f.db, w, DeferredWork, metricFixtureRow{"discarded", 3, now, &now, &now})
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET last_error='handler_failed' WHERE id=?", mysqlstore.UUID(id))
	op, err := NewWorkOperator(w, func(context.Context, *sql.Tx, WorkOperatorScope) (string, error) { return "fixture-operator", nil })
	if err != nil {
		t.Fatal(err)
	}
	successor, err := op.Redrive(ctx, RedriveWorkRequest{ResourceID: id, PreviousJobID: id, ActionID: uuid.New(), ReasonCode: "dependency_recovered", ScheduledAt: now})
	if err != nil {
		t.Fatal(err)
	}
	fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='completed',attempt=1,started_at=?,finished_at=? WHERE id=?", now, now, mysqlstore.UUID(successor))
	fixtureExec(t, f.db, "UPDATE ingestion_work_resources SET completed_at=? WHERE id=?", now, mysqlstore.UUID(id))
	fixtureExec(t, f.db, "DELETE FROM ingestion_work_reservations WHERE resource_id=?", mysqlstore.UUID(id))
	got, err := w.DeferredMetricsSnapshot(ctx, 1)
	if err != nil || got.CompletedCount != 1 || got.FailedCount != 0 || got.RetriedExecutionCount != 0 || got.RecentSuccessCount != 1 {
		t.Fatal("historical job counted", got, err)
	}
	// The original job still exists. An absent original current job is corruption.
	missing := seedMetricRow(t, f.db, w, DeferredWork, metricFixtureRow{"completed", 1, now, &now, &now})
	fixtureExec(t, f.db, "DELETE FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(missing))
	if got, err := w.DeferredMetricsSnapshot(ctx, 2); !errors.Is(err, ErrWorkMetricsIntegrity) || !reflect.DeepEqual(got, ingestion.DeferredIngestMetrics{}) {
		t.Fatal("missing job hidden", got, err)
	}
}

func TestLiveParityDeferredMetrics(t *testing.T) {
	_, f, w, rows := metricFixture(t)
	pg, _ := paritySource(t, f.model)
	ctx := context.Background()
	// The fixed parity server is disposable. Create exclusively and clean up only
	// after successful ownership; never reuse or drop another suite's schema.
	if _, err := pg.Exec(ctx, "CREATE SCHEMA core_ingestion"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pg.Exec(context.Background(), "DROP SCHEMA core_ingestion CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if _, err := pg.Exec(ctx, "CREATE TABLE core_ingestion.deferred_ingests(status text,attempt_count int,requested_at timestamptz,started_at timestamptz,completed_at timestamptz)"); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		state := map[string]string{"available": "queued", "running": "processing", "completed": "completed", "discarded": "failed", "cancelled": "failed"}[row.state]
		if _, err := pg.Exec(ctx, "INSERT INTO core_ingestion.deferred_ingests VALUES($1,$2,$3,$4,$5)", state, row.attempt, row.requested, row.started, row.finished); err != nil {
			t.Fatal(err)
		}
	}
	got, err := w.DeferredMetricsSnapshot(WithServiceCredential(ctx, "fixture-token"), 8)
	if err != nil {
		t.Fatal(err)
	}
	want, err := postgres.NewDeferredIngestRepository(pg).MetricsSnapshot(ctx, got.SnapshotAt)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OldestQueuedRequestedAt.Equal(*want.OldestQueuedRequestedAt) {
		t.Fatal(got, want)
	}
	got.OldestQueuedRequestedAt, want.OldestQueuedRequestedAt = nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MySQL=%+v PostgreSQL=%+v", got, want)
	}
}
