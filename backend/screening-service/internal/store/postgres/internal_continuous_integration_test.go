package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	legacy "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerregistry"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcemonitor"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"testing"
	"time"
)

func TestInternalContinuousLiveObjectPublicationFanoutAndRevocation(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE screening.test_source_queue(tenant_id uuid,screening_id uuid)`)
	registry := providerregistry.New(f.db, legacy.NewHTTPClient("", nil, time.Second, legacy.OpenSanctionsConfig{}))
	producer := service.NewScreeningService(store.NewTransactionManager(f.db), deliveryTestIDs{}, deliveryTestClock{}, store.NewScreeningRepository(f.db), store.NewScreeningMatchRepository(f.db), store.NewScreeningCommentRepository(f.db), store.NewScreeningWhitelistRepository(f.db), store.NewScreeningFileRepository(f.db), store.NewContinuousConfigRepository(f.db), store.NewMonitoredObjectRepository(f.db), store.NewDatasetUpdateJobRepository(f.db), registry, nil, nil, nil, nil, sourceTestQueue{}, nil, sourceTestQueue{})
	worker := service.NewContinuousWorkerService(store.NewTransactionManager(f.db), deliveryTestClock{}, store.NewContinuousConfigRepository(f.db), store.NewMonitoredObjectRepository(f.db), intakeReader{}, producer)
	dispatch := service.NewDispatchService(store.NewTransactionManager(f.db), deliveryTestClock{}, store.NewScreeningRepository(f.db), store.NewScreeningMatchRepository(f.db), registry, nil)
	mapping, _ := json.Marshal(sourcedata.ContinuousMapping{Version: 2, Fields: map[string]string{"name": "Person"}, Selection: sourcedata.Selection{Version: 2, Profile: sourcedata.Profile, Threshold: 100, Datasets: []sourcedata.DatasetRef{{Scope: "tenant", ID: f.private.Dataset}}}})
	config, e := producer.CreateContinuousConfig(ctx, f.a.Tenant().String(), "Live customer check", "customer", sourcedata.Provider, mapping, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	item, e := producer.CreateMonitoredObject(ctx, config.TenantID, config.ID, "customer", "live-1", json.RawMessage(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	if e = worker.RunMonitoredObject(ctx, item.TenantID, item.ID); e != nil {
		t.Fatal(e)
	}
	var origin string
	if e = f.db.QueryRow(ctx, `SELECT screening_id::text FROM screening.continuous_runs WHERE monitored_object_id=$1`, item.ID).Scan(&origin); e != nil {
		t.Fatal(e)
	}
	if e = dispatch.RunScreening(ctx, item.TenantID, origin); e != nil {
		t.Fatal(e)
	}
	first, e := store.NewScreeningRepository(f.db).GetByID(ctx, item.TenantID, origin)
	if e != nil || first.Status != screening.StatusNoHit {
		t.Fatal(first, e)
	}
	exec(`INSERT INTO screening.monitored_objects(id,tenant_id,config_id,object_type,object_id,status,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,'customer','bulk-'||n,'active',now(),now() FROM generate_series(1,602)n`, item.TenantID, config.ID)
	raw, _ := json.Marshal(struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}{1, "controlled-fixture", []sourcedata.Record{{ID: "new", Name: "Customer", Type: "Person", SourceReference: "fictional live object test"}}})
	ingest := sourceingest.Store{DB: f.db}
	generation, e := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, f.private, generation); e != nil {
		t.Fatal(e)
	}
	failing := sourcemonitor.Service{DB: f.db, MonitoredEnqueue: sourceTestQueue{fail: true}}
	for i := 0; i < 3; i++ {
		if _, e = failing.FanoutContinuousPage(ctx); e != nil {
			break
		}
	}
	if e == nil {
		t.Fatal("queue outage hidden")
	}
	var count int
	f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_continuous_receipts`).Scan(&count)
	if count != 0 {
		t.Fatal("page escaped rollback", count)
	}
	restarted := sourcemonitor.Service{DB: f.db, MonitoredEnqueue: sourceTestQueue{}}
	for i := 0; i < 10; i++ {
		if _, e = restarted.FanoutContinuousPage(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if e = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_continuous_receipts r JOIN screening.source_publications p ON p.id=r.publication_id WHERE p.generation=$1`, generation).Scan(&count); e != nil || count != 603 {
		t.Fatal("lost or duplicate live objects", count, e)
	}
	if e = worker.RunMonitoredObject(ctx, item.TenantID, item.ID); e != nil {
		t.Fatal(e)
	}
	var latest string
	if e = f.db.QueryRow(ctx, `SELECT screening_id::text FROM screening.continuous_runs WHERE monitored_object_id=$1 AND screening_id<>$2`, item.ID, origin).Scan(&latest); e != nil {
		t.Fatal(e)
	}
	if e = dispatch.RunScreening(ctx, item.TenantID, latest); e != nil {
		t.Fatal(e)
	}
	second, e := store.NewScreeningRepository(f.db).GetByID(ctx, item.TenantID, latest)
	if e != nil || second.Status != screening.StatusAwaitingReview {
		t.Fatal("live object missed source addition", second.LastError, e)
	}
	if old, e := store.NewScreeningRepository(f.db).GetByID(ctx, item.TenantID, origin); e != nil || !bytes.Equal(old.ResponseJSON, first.ResponseJSON) {
		t.Fatal("old source evidence rewritten", e)
	}
	exec(`UPDATE screening.monitored_objects SET status='pending' WHERE id=$1`, item.ID)
	if e = worker.RunMonitoredObject(ctx, item.TenantID, item.ID); e != nil {
		t.Fatal(e)
	}
	var denied string
	f.db.QueryRow(ctx, `SELECT screening_id::text FROM screening.continuous_runs WHERE monitored_object_id=$1 AND screening_id NOT IN($2,$3)`, item.ID, origin, latest).Scan(&denied)
	exec(`DELETE FROM screening.source_grants WHERE tenant_id=$1 AND owner_scope=$1 AND dataset_id=$2`, f.a.Tenant(), f.private.Dataset)
	if e = dispatch.RunScreening(ctx, item.TenantID, denied); e != nil {
		t.Fatal(e)
	}
	result, e := store.NewScreeningRepository(f.db).GetByID(ctx, item.TenantID, denied)
	if e != nil || result.Status != screening.StatusFailed {
		t.Fatal("revoked live object source became clean", result, e)
	}
}
