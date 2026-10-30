package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestFullSharedDeferredMetrics(t *testing.T) {
	shared, f := fullRecordFixture(t)
	owner, _ := fullOwner(t, shared, f, "worker")
	w, e := NewWorkOwner(owner, "deferred", "deferred", 1<<20, 3)
	if e != nil {
		t.Fatal(e)
	}
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	completed := workInput(DeferredWork)
	if _, e = w.Create(ctx, completed); e != nil {
		t.Fatal(e)
	}
	if e = w.Handle(ctx, claimWork(t, w, DeferredWork)); e != nil {
		t.Fatal(e)
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, ExecutionReceipts: 1, Audits: 1, Outbox: 1, Resources: 1, Jobs: 1}) {
		t.Fatal(got)
	}
	for _, state := range []string{"available", "running", "discarded", "cancelled"} {
		in := workInput(DeferredWork)
		if _, e = w.Create(ctx, in); e != nil {
			t.Fatal(e)
		}
		switch state {
		case "running":
			fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state='running',lease_token=UUID_TO_BIN(UUID()),lease_until=UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE WHERE id=?", mysqlstore.UUID(in.ID))
		case "discarded", "cancelled":
			fixtureExec(t, f.db, "UPDATE ingestion_work_jobs SET state=?,finished_at=UTC_TIMESTAMP(6) WHERE id=?", state, mysqlstore.UUID(in.ID))
		}
	}
	if _, e = w.Create(ctx, workInput(UploadWork)); e != nil {
		t.Fatal(e)
	}
	pool := fullFixturePrincipal(t, shared, "operator")
	var account string
	if e := pool.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&account); e != nil {
		t.Fatal(e)
	}
	fixtureExec(t, f.db, "GRANT SELECT(id,status) ON "+shared.Database+".data_model_tenants TO '"+account+"'@'%'")
	r, e := NewWorkInspector(pool, shared.Database, f.model.TenantID, func(ctx context.Context, tx *sql.Tx, s QueueInspectionScope) error {
		token, _ := ctx.Value(credentialContextKey{}).(string)
		if token != "fixture-token" || s.TenantID != f.model.TenantID || s.Queue != "deferred" {
			return ErrOwnerAuthorization
		}
		var status string
		if e := tx.QueryRowContext(ctx, "SELECT status FROM "+shared.Database+".data_model_tenants WHERE id=?", mysqlstore.UUID(s.TenantID)).Scan(&status); e != nil {
			return e
		}
		if status != "active" {
			return ErrOwnerAuthorization
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	counts, e := r.DeferredMetrics(ctx, "deferred", 1)
	if e != nil || counts != (DeferredWorkMetrics{1, 1, 1, 1, 1}) || counts.Total() != 5 {
		t.Fatal(counts, e)
	}
	if c, e := r.DeferredMetrics(context.Background(), "deferred", 10); !errors.Is(e, ErrOwnerAuthorization) || c.Total() != 0 {
		t.Fatal(c, e)
	}
	if c, e := r.DeferredMetrics(ctx, "other", 10); !errors.Is(e, ErrOwnerAuthorization) || c.Total() != 0 {
		t.Fatal(c, e)
	}
	if _, e = w.Create(ctx, workInput(DeferredWork)); e != nil {
		t.Fatal(e)
	}
	if c, e := r.DeferredMetrics(ctx, "deferred", 1); !errors.Is(e, ErrWorkMetricsBound) || c.Total() != 0 {
		t.Fatal(c, e)
	}
	var plan string
	if e := f.db.QueryRow("EXPLAIN FORMAT=JSON SELECT id FROM ingestion_work_jobs FORCE INDEX(deferred_metrics) WHERE tenant_id=? AND queue=? AND kind=? AND state=? LIMIT 2", mysqlstore.UUID(f.model.TenantID), "deferred", DeferredWork, "available").Scan(&plan); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(plan, `"key": "deferred_metrics"`) {
		t.Fatal(plan)
	}
	t.Log("bounded deferred count range plan:", plan)
	for _, q := range []string{"SELECT payload FROM ingestion_work_jobs", "SELECT lease_token FROM ingestion_work_jobs", "UPDATE data_model_tenants SET status='pending'", "SELECT raw_value FROM data_model_exact_keys", "CREATE TABLE forbidden(id INT)"} {
		if _, e := pool.Exec(q); e == nil {
			t.Fatal("forbidden operator access", q)
		}
	}
}
