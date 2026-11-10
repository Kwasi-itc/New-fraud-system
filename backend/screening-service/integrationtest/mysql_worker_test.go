package integrationtest

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	api "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	runtime "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	native "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"testing"
)

type records struct{ fail bool }

func (r records) GetRecord(ctx context.Context, t, kind, key string) (ports.TenantRecord, error) {
	if r.fail {
		return ports.TenantRecord{}, errors.New("injected record outage")
	}
	return ports.TenantRecord{ObjectID: key, ObjectType: kind, Fields: map[string]any{"name": "Alex Example"}}, ctx.Err()
}

type publisher struct{ events int }

func (p *publisher) PublishScreeningRequiresReview(context.Context, ports.ScreeningRequiresReviewCommand) error {
	p.events++
	return nil
}
func (p *publisher) PublishScreeningReviewed(context.Context, ports.ScreeningReviewedCommand) error {
	p.events++
	return nil
}
func (p *publisher) PublishScreeningEvidenceUploaded(context.Context, ports.ScreeningEvidenceUploadedCommand) error {
	p.events++
	return nil
}
func workerFixture(t *testing.T, f *fixture, reader records, pub *publisher) runtime.Worker {
	db := f.runtimeDB(t, "worker")
	store, e := native.New(db, f.database)
	must(t, e)
	deps := api.MySQLDependencies{Provider: runtime.LocalProvider{}, Inbox: store}
	svc := api.MySQLService(store, deps)
	delivery := service.NewCaseDeliveryService(store, pub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return runtime.Worker{Store: store, DB: db, Database: f.database, Dispatch: service.NewDispatchService(store, clock{}, store.Screenings(), store.ScreeningMatches(), deps.Provider, nil), Continuous: service.NewContinuousWorkerService(store, clock{}, store.ContinuousConfigs(), store.MonitoredObjects(), reader, svc), Datasets: service.NewDatasetUpdateWorkerService(store, clock{}, store.DatasetUpdateJobs(), store.ContinuousConfigs(), store.MonitoredObjects(), deps.Provider, native.DomainPollingEnqueuer{}), Delivery: &delivery}
}
func TestMySQLScreeningContinuousAndDatasetWorkers(t *testing.T) {
	f := fixtureMySQL(t)
	svc := api.MySQLService(f.store, api.MySQLDependencies{Provider: runtime.LocalProvider{}, Inbox: f.store})
	inbox := f.inbox.String()
	cfg, e := svc.CreateContinuousConfig(f.ctx, f.tenant.String(), "Synthetic continuous checks", "Person", "opensanctions", json.RawMessage(`{"name":"Person"}`), &inbox, true)
	must(t, e)
	obj, e := svc.CreateMonitoredObject(f.ctx, f.tenant.String(), cfg.ID, "Person", "person-001", json.RawMessage(`{}`))
	must(t, e)
	job, e := svc.CreateDatasetUpdateJob(f.ctx, f.tenant.String(), "opensanctions", "catalog_sync", "")
	must(t, e)
	pub := &publisher{}
	worker := workerFixture(t, f, records{}, pub)
	must(t, worker.Tick(f.ctx))
	active, e := f.store.MonitoredObjects().GetByID(f.ctx, f.tenant.String(), obj.ID)
	must(t, e)
	if active.Status != "active" {
		t.Fatal("monitored object was not screened")
	}
	finished, e := f.store.DatasetUpdateJobs().GetByID(f.ctx, f.tenant.String(), job.ID)
	must(t, e)
	if finished.Status != "completed" {
		t.Fatal("dataset job incomplete")
	}
	must(t, worker.Tick(f.ctx))
	if pub.events != 1 {
		t.Fatalf("continuous arrival count=%d", pub.events)
	}
	var sourceCount int
	must(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM `"+f.database+"`.screening_continuous_runs WHERE tenant_id=?", mysqlstore.UUID(f.tenant)).Scan(&sourceCount))
	if sourceCount != 1 {
		t.Fatal("missing continuous provenance")
	}
	_, e = svc.CreateDatasetUpdateJob(f.ctx, f.tenant.String(), "opensanctions", "rescreen_monitored_objects", "")
	must(t, e)
	must(t, worker.Tick(f.ctx))
	again, e := f.store.MonitoredObjects().GetByID(f.ctx, f.tenant.String(), obj.ID)
	must(t, e)
	if again.Status != "pending" {
		t.Fatal("dataset rescreen did not queue the object")
	}
	must(t, worker.Tick(f.ctx))
	must(t, worker.Tick(f.ctx))
	if pub.events != 2 {
		t.Fatalf("rescreen arrival count=%d", pub.events)
	}
}
func TestMySQLScreeningWorkerBackoffRecoveryAndFencing(t *testing.T) {
	f := fixtureMySQL(t)
	svc := api.MySQLService(f.store, api.MySQLDependencies{Provider: runtime.LocalProvider{}, Inbox: f.store})
	cfg, e := svc.CreateContinuousConfig(f.ctx, f.tenant.String(), "Recovery fixture", "Person", "opensanctions", json.RawMessage(`{"name":"Person"}`), nil, true)
	must(t, e)
	obj, e := svc.CreateMonitoredObject(f.ctx, f.tenant.String(), cfg.ID, "Person", "person-002", json.RawMessage(`{}`))
	must(t, e)
	worker := workerFixture(t, f, records{fail: true}, &publisher{})
	must(t, worker.Tick(f.ctx))
	ready, e := f.store.MonitoredObjects().ListByStatus(f.ctx, "pending", 25)
	must(t, e)
	if len(ready) != 0 {
		t.Fatal("backed-off poison work remains at the queue head")
	}
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_work_leases SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND kind='continuous' AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(obj.ID)))
	must(t, e)
	worker = workerFixture(t, f, records{}, &publisher{})
	must(t, worker.Tick(f.ctx))
	active, e := f.store.MonitoredObjects().GetByID(f.ctx, f.tenant.String(), obj.ID)
	must(t, e)
	if active.Status != "active" {
		t.Fatal("expired continuous lease did not recover")
	}
	job, e := svc.CreateDatasetUpdateJob(f.ctx, f.tenant.String(), "opensanctions", "catalog_sync", "")
	must(t, e)
	old, e := f.store.ClaimWork(f.ctx, f.tenant.String(), "dataset", job.ID)
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_dataset_jobs SET status='processing' WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(job.ID)))
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_work_leases SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND kind='dataset' AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(job.ID)))
	must(t, e)
	stale := native.WithWork(f.ctx, f.tenant.String(), "dataset", job.ID, old)
	e = f.store.Run(stale, func(ports.MutationStore) error { return nil })
	if !errors.Is(e, ports.ErrScreeningBusy) {
		t.Fatal("expired worker retained mutation authority")
	}
	must(t, worker.Tick(f.ctx))
	recovered, e := f.store.DatasetUpdateJobs().GetByID(f.ctx, f.tenant.String(), job.ID)
	must(t, e)
	if recovered.Status != "completed" {
		t.Fatal("processing dataset job did not recover")
	}
}
func TestMySQLScreeningTerminalCallbackFailure(t *testing.T) {
	f := fixtureMySQL(t)
	event := uuid.NewString()
	must(t, f.store.Run(f.ctx, func(m ports.MutationStore) error {
		return m.CaseEvents().Enqueue(f.ctx, event, f.tenant.String(), "requires-review", ports.ScreeningRequiresReviewCommand{Version: 1, EventID: event, TenantID: f.tenant.String(), ScreeningID: uuid.NewString()})
	}))
	for i := 0; i < 12; i++ {
		v, e := f.store.Claim(f.ctx)
		must(t, e)
		if v == nil {
			t.Fatal("missing callback attempt")
		}
		must(t, f.store.Finish(f.ctx, *v, errors.New("injected transport failure")))
		_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_case_event_outbox SET next_attempt_at=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(event)))
		must(t, e)
	}
	v, e := f.store.Claim(f.ctx)
	must(t, e)
	if v != nil {
		t.Fatal("terminal callback was retried automatically")
	}
	must(t, f.store.ReplayCallback(f.ctx, f.tenant.String(), event))
	v, e = f.store.Claim(f.ctx)
	must(t, e)
	if v == nil || v.ID != event {
		t.Fatal("terminal replay lost stable identity")
	}
	must(t, f.store.Finish(f.ctx, *v, nil))
}
