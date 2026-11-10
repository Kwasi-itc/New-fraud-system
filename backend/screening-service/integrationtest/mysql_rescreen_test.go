package integrationtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	api "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	runtime "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	native "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/google/uuid"
	"strings"
	"sync/atomic"
	"testing"
)

type deltaCounter struct {
	runtime.LocalProvider
	calls atomic.Int32
}

func (p *deltaCounter) GetDatasetDelta(ctx context.Context, provider, cursor string) (d.DatasetDelta, error) {
	p.calls.Add(1)
	return p.LocalProvider.GetDatasetDelta(ctx, provider, cursor)
}

func TestMySQLScreeningRescreenPaginationAndRecovery(t *testing.T) {
	f := fixtureMySQL(t)
	tenant := f.tenant.String()
	schema := "`" + f.database + "`"
	svc := api.MySQLService(f.store, api.MySQLDependencies{Provider: runtime.LocalProvider{}, Inbox: f.store})
	cfg, e := svc.CreateContinuousConfig(f.ctx, tenant, "Paged rescreen fixture", "Person", "opensanctions", json.RawMessage(`{"name":"Person"}`), nil, true)
	must(t, e)
	other, e := svc.CreateContinuousConfig(f.ctx, tenant, "Different provider", "Person", "other", json.RawMessage(`{"name":"Person"}`), nil, true)
	must(t, e)
	disabled, e := svc.CreateContinuousConfig(f.ctx, tenant, "Disabled config", "Person", "opensanctions", json.RawMessage(`{"name":"Person"}`), nil, false)
	must(t, e)
	for start := 1; start <= 640; start += 100 {
		values := []string{}
		args := []any{}
		for i := start; i < start+100 && i <= 640; i++ {
			config, status := cfg.ID, "active"
			var deleted any
			if i > 625 && i <= 630 {
				config = other.ID
			}
			if i > 630 && i <= 635 {
				config = disabled.ID
			}
			if i > 635 {
				status = "paused"
			}
			key := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", i))
			values = append(values, "(?,?,?,'Person',?,?,JSON_OBJECT(),?,TIMESTAMPADD(MINUTE,-5,UTC_TIMESTAMP(6)),UTC_TIMESTAMP(6))")
			args = append(args, mysqlstore.UUID(f.tenant), mysqlstore.UUID(key), mysqlstore.UUID(uuid.MustParse(config)), fmt.Sprintf("synthetic-%d", i), status, deleted)
		}
		_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".screening_monitored_objects(tenant_id,id,config_id,object_type,object_id,status,attributes_json,deleted_at,created_at,updated_at) VALUES"+strings.Join(values, ","), args...)
		must(t, e)
	}
	job, e := svc.CreateDatasetUpdateJob(f.ctx, tenant, "opensanctions", "dataset_delta_rescreen", "")
	must(t, e)
	// Future objects are outside this job's creation cutoff.
	future := uuid.New()
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO "+schema+".screening_monitored_objects(tenant_id,id,config_id,object_type,object_id,status,attributes_json,created_at,updated_at) VALUES(?,?,?,'Person','future','active',JSON_OBJECT(),TIMESTAMPADD(MINUTE,10,UTC_TIMESTAMP(6)),UTC_TIMESTAMP(6))", mysqlstore.UUID(f.tenant), mysqlstore.UUID(future), mysqlstore.UUID(uuid.MustParse(cfg.ID)))
	must(t, e)
	provider := &deltaCounter{}
	workerDB := f.runtimeDB(t, "worker")
	workerStore, e := native.New(workerDB, f.database)
	must(t, e)
	runner := func() service.DatasetUpdateWorkerService {
		return service.NewDatasetUpdateWorkerService(workerStore, clock{}, workerStore.DatasetUpdateJobs(), workerStore.ContinuousConfigs(), workerStore.MonitoredObjects(), provider, native.DomainPollingEnqueuer{})
	}
	token, e := f.store.ClaimWork(f.ctx, tenant, "dataset", job.ID)
	must(t, e)
	work := native.WithWork(f.ctx, tenant, "dataset", job.ID, token)
	must(t, runner().RunJob(work, tenant, job.ID))
	partial, e := f.store.DatasetUpdateJobs().GetByID(f.ctx, tenant, job.ID)
	must(t, e)
	if partial.Status != "pending" || partial.Cursor != "" {
		t.Fatal("partial job advanced provider cursor or lost continuation")
	}
	progressRepo := workerStore.DatasetUpdateJobs().(ports.ResumableDatasetJobs)
	saved, e := progressRepo.RescreenProgress(f.ctx, tenant, job.ID)
	must(t, e)
	if saved.Requeued != 50 || saved.Completed {
		t.Fatal("first bounded page did not checkpoint exactly 50 objects")
	}
	// Simulate those objects being processed while this dataset job is interrupted.
	_, e = f.db.ExecContext(f.ctx, "UPDATE "+schema+".screening_monitored_objects SET status='active' WHERE tenant_id=? AND status='pending'", mysqlstore.UUID(f.tenant))
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "UPDATE "+schema+".screening_work_leases SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND kind='dataset' AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(job.ID)))
	must(t, e)
	_, e = progressRepo.AdvanceRescreen(work, partial, saved.Plan, 50)
	if !errors.Is(e, ports.ErrScreeningBusy) {
		t.Fatal("stale worker advanced rescreen checkpoint", e)
	}
	for i := 0; i < 20; i++ {
		token, e = f.store.ClaimWork(f.ctx, tenant, "dataset", job.ID)
		must(t, e)
		work = native.WithWork(f.ctx, tenant, "dataset", job.ID, token)
		// Simulate the processing-row recovery used by the native runtime.
		if i == 0 {
			_, e = f.db.ExecContext(f.ctx, "UPDATE "+schema+".screening_dataset_jobs SET status='processing' WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(job.ID)))
			must(t, e)
			must(t, f.store.Run(work, func(m ports.MutationStore) error { return m.(*native.Store).ResetDataset(work, tenant, job.ID) }))
		}
		must(t, runner().RunJob(work, tenant, job.ID))
		must(t, f.store.ReleaseWork(f.ctx, tenant, "dataset", job.ID, token))
		current, e := f.store.DatasetUpdateJobs().GetByID(f.ctx, tenant, job.ID)
		must(t, e)
		if current.Status == "completed" {
			if current.Cursor != "catalog-v1" {
				t.Fatal("completed job lost provider cursor")
			}
			break
		}
	}
	saved, e = progressRepo.RescreenProgress(f.ctx, tenant, job.ID)
	must(t, e)
	if !saved.Completed || saved.Requeued != 625 || provider.calls.Load() != 1 {
		t.Fatalf("rescreen recovery: completed=%v count=%d providerCalls=%d", saved.Completed, saved.Requeued, provider.calls.Load())
	}
	var pending, active int
	must(t, f.db.QueryRowContext(f.ctx, "SELECT SUM(status='pending'),SUM(status='active') FROM "+schema+".screening_monitored_objects WHERE tenant_id=? AND config_id=? AND id<>?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(cfg.ID)), mysqlstore.UUID(future)).Scan(&pending, &active))
	if pending != 575 || active != 50 {
		t.Fatal("checkpoint replayed already processed objects", pending, active)
	}
	var untouched int
	must(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM "+schema+".screening_monitored_objects WHERE tenant_id=? AND status='pending' AND(config_id<>? OR id=?)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(cfg.ID)), mysqlstore.UUID(future)).Scan(&untouched))
	if untouched != 0 {
		t.Fatal("rescreen touched a disabled, other-provider, paused or future object")
	}
	unchanged, e := svc.CreateDatasetUpdateJob(f.ctx, tenant, "opensanctions", "dataset_delta_rescreen", "catalog-v1")
	must(t, e)
	token, e = f.store.ClaimWork(f.ctx, tenant, "dataset", unchanged.ID)
	must(t, e)
	work = native.WithWork(f.ctx, tenant, "dataset", unchanged.ID, token)
	must(t, runner().RunJob(work, tenant, unchanged.ID))
	must(t, f.store.ReleaseWork(f.ctx, tenant, "dataset", unchanged.ID, token))
	nochange, e := progressRepo.RescreenProgress(f.ctx, tenant, unchanged.ID)
	must(t, e)
	if !nochange.Completed || nochange.Requeued != 0 {
		t.Fatal("unchanged provider snapshot requeued objects")
	}
}
