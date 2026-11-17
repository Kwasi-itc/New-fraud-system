package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	legacy "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type maintenanceQueue struct{ failAfter200 bool }

func (q *maintenanceQueue) Enqueue(context.Context, string, string, *time.Time) error {
	return errors.New("transaction required")
}
func (q *maintenanceQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, tenant, id string, _ *time.Time) error {
	if q.failAfter200 {
		var count int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM screening.test_maintenance_queue`).Scan(&count); e != nil {
			return e
		}
		if count >= 200 {
			return errors.New("injected second-page queue failure")
		}
	}
	_, e := tx.Exec(ctx, `INSERT INTO screening.test_maintenance_queue(tenant_id,object_id) VALUES($1,$2)`, tenant, id)
	return e
}
func TestDatasetMaintenanceDrainsPagesAndResumesAtomicObjectCheckpoint(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	tenant := f.a.Tenant().String()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE screening.test_maintenance_queue(tenant_id uuid NOT NULL,object_id uuid PRIMARY KEY)`)
	configA, configB, job := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO screening.continuous_screening_configs(id,tenant_id,provider,name,object_type,field_map_json,enabled,created_at,updated_at) VALUES($1,$2,'fixture','A','customer','{}',true,now(),now()),($3,$2,'other','B','customer','{}',true,now(),now())`, configA, tenant, configB)
	exec(`INSERT INTO screening.monitored_objects(id,tenant_id,config_id,object_type,object_id,status,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'customer','object-'||n,'active',now(),now() FROM generate_series(1,603)n`, tenant, configA)
	exec(`INSERT INTO screening.monitored_objects(id,tenant_id,config_id,object_type,object_id,status,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'customer','other-'||n,'active',now(),now() FROM generate_series(1,50)n`, tenant, configB)
	exec(`INSERT INTO screening.dataset_update_jobs(id,tenant_id,provider,job_type,status,cursor,created_at,updated_at) VALUES($1,$2,'fixture','dataset_delta_rescreen','pending','start',now(),now())`, job, tenant)
	var calls atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cursor := r.URL.Query().Get("cursor")
		next, more, changed := "", false, false
		switch cursor {
		case "start":
			next, more = "page-2", true
		case "page-2":
			next, more, changed = "page-3", true, true
		case "page-3":
			next = "last"
		default:
			http.Error(w, "unexpected cursor", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"next_cursor": next, "has_more": more, "changed": changed})
	}))
	defer endpoint.Close()
	provider := legacy.NewHTTPClient("", map[string]string{"fixture": endpoint.URL}, time.Second, legacy.OpenSanctionsConfig{})
	queue := &maintenanceQueue{failAfter200: true}
	worker := func() service.DatasetUpdateWorkerService {
		return service.NewDatasetUpdateWorkerService(store.NewTransactionManager(f.db), deliveryTestClock{}, store.NewDatasetUpdateJobRepository(f.db), store.NewContinuousConfigRepository(f.db), store.NewMonitoredObjectRepository(f.db), provider, queue)
	}
	if e := worker().RunJob(ctx, tenant, job); e != nil {
		t.Fatal(e)
	}
	var status, cursor string
	var queued, receipts int
	if e := f.db.QueryRow(ctx, `SELECT j.status,j.cursor,p.queued_objects,(SELECT count(*) FROM screening.test_maintenance_queue) FROM screening.dataset_update_jobs j JOIN screening.dataset_maintenance_progress p ON(p.tenant_id,p.job_id)=(j.tenant_id,j.id) WHERE j.id=$1`, job).Scan(&status, &cursor, &queued, &receipts); e != nil || status != "failed" || cursor != "last" || queued != 200 || receipts != 200 || calls.Load() != 3 {
		t.Fatal("failed page advanced or delta not drained", status, cursor, queued, receipts, calls.Load(), e)
	}
	queue.failAfter200 = false
	exec(`UPDATE screening.dataset_update_jobs SET status='pending' WHERE id=$1`, job)
	if e := worker().RunJob(ctx, tenant, job); e != nil {
		t.Fatal("resume", e)
	}
	if e := f.db.QueryRow(ctx, `SELECT j.status,p.queued_objects,(SELECT count(*) FROM screening.dataset_maintenance_receipts WHERE job_id=$1),(SELECT count(*) FROM screening.test_maintenance_queue) FROM screening.dataset_update_jobs j JOIN screening.dataset_maintenance_progress p ON(p.tenant_id,p.job_id)=(j.tenant_id,j.id) WHERE j.id=$1`, job).Scan(&status, &queued, &receipts, new(int)); e != nil || status != "completed" || queued != 603 || receipts != 603 || calls.Load() != 3 {
		t.Fatal("resume lost or duplicated pages", status, queued, receipts, calls.Load(), e)
	}
	if e := worker().RunJob(ctx, tenant, job); e != nil {
		t.Fatal("completed replay", e)
	}
	var untouched int
	if e := f.db.QueryRow(ctx, `SELECT count(*) FROM screening.monitored_objects WHERE config_id=$1 AND status='active'`, configB).Scan(&untouched); e != nil || untouched != 50 {
		t.Fatal("other provider requeued", untouched, e)
	}
	// A dead process leaves a durable lease. Recovery admits a new worker, while
	// the previous lease token cannot checkpoint or finish its obsolete work.
	exec(`TRUNCATE screening.test_maintenance_queue`)
	nextJob, oldToken := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO screening.dataset_update_jobs(id,tenant_id,provider,job_type,status,created_at,updated_at) VALUES($1,$2,'fixture','rescreen_monitored_objects','pending',now(),now())`, nextJob, tenant)
	tx, e := f.db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, claimed, e := store.NewDatasetUpdateJobRepository(tx).ClaimMaintenance(ctx, tenant, nextJob, oldToken, time.Now())
	if e != nil || !claimed {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE screening.dataset_maintenance_progress SET lease_until=now()-interval '1 second' WHERE job_id=$1`, nextJob)
	if e = worker().ProcessPendingJobs(ctx, 10); e != nil {
		t.Fatal("expired lease recovery", e)
	}
	tx, e = f.db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	e = store.NewDatasetUpdateJobRepository(tx).FinishMaintenance(ctx, tenant, nextJob, oldToken, "completed", "", time.Now())
	tx.Rollback(ctx)
	if e == nil {
		t.Fatal("old worker replaced newer completion")
	}
	if _, e = f.db.Exec(ctx, `INSERT INTO screening.dataset_maintenance_receipts(tenant_id,job_id,config_id,object_id) SELECT $1,$2,config_id,id FROM screening.monitored_objects LIMIT 1`, f.b.Tenant(), job); e == nil {
		t.Fatal("cross-tenant maintenance receipt")
	}
	var _ ports.DatasetMaintenanceRepository = store.NewDatasetUpdateJobRepository(f.db)
}
