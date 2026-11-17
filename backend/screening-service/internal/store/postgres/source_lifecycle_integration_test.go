package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	legacy "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerregistry"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcemonitor"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type sourceTestQueue struct{ fail bool }

func (q sourceTestQueue) Enqueue(context.Context, string, string, *time.Time) error {
	return errors.New("transaction required")
}
func (q sourceTestQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, tenant, id string, _ *time.Time) error {
	if q.fail {
		return errors.New("injected queue outage")
	}
	_, err := tx.Exec(ctx, `INSERT INTO screening.test_source_queue(tenant_id,screening_id) VALUES($1,$2)`, tenant, id)
	return err
}

func TestSourcePublicationRescreensBeyond500AndTracksCurrentness(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE screening.test_source_queue(tenant_id uuid,screening_id uuid)`)
	registry := providerregistry.New(f.db, legacy.NewHTTPClient("", nil, time.Second, legacy.OpenSanctionsConfig{}))
	svc := service.NewScreeningService(store.NewTransactionManager(f.db), deliveryTestIDs{}, deliveryTestClock{}, store.NewScreeningRepository(f.db), store.NewScreeningMatchRepository(f.db), store.NewScreeningCommentRepository(f.db), store.NewScreeningWhitelistRepository(f.db), store.NewScreeningFileRepository(f.db), store.NewContinuousConfigRepository(f.db), store.NewMonitoredObjectRepository(f.db), store.NewDatasetUpdateJobRepository(f.db), registry, nil, nil, nil, nil, sourceTestQueue{}, nil, nil)
	dispatch := service.NewDispatchService(store.NewTransactionManager(f.db), deliveryTestClock{}, store.NewScreeningRepository(f.db), store.NewScreeningMatchRepository(f.db), registry, nil)
	scoped := provideraccess.WithScope(ctx, f.a)
	input := service.ManualSearchInput{Provider: sourcedata.Provider, Profile: sourcedata.Profile, DatasetSelection: []sourcedata.DatasetRef{{Scope: "tenant", ID: f.private.Dataset}}, OperationID: uuid.NewString(), Name: "Future Designation", EntityType: "Person", Threshold: 100, Limit: 10}
	origin, err := svc.CreateManualSearch(scoped, f.a.Tenant().String(), f.a.Principal().Subject, input)
	if err != nil {
		t.Fatal(err)
	}
	if err = dispatch.RunScreening(ctx, origin.TenantID, origin.ID); err != nil {
		t.Fatal(err)
	}
	original, err := store.NewScreeningRepository(f.db).GetByID(ctx, origin.TenantID, origin.ID)
	if err != nil || original.Status != screening.StatusNoHit {
		t.Fatal(original.Status, err)
	}
	monitor := sourcemonitor.Service{DB: f.db, Enqueue: sourceTestQueue{}}
	monitorID, err := monitor.Enroll(ctx, f.a.Principal(), uuid.MustParse(origin.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = monitor.Enroll(ctx, f.b.Principal(), uuid.MustParse(origin.ID)); err == nil {
		t.Fatal("foreign tenant enrolled private origin")
	}
	// Complete the initial reconciliation through the same real dispatch/provider.
	if err = monitor.RunBatch(ctx, 50); err != nil {
		t.Fatal(err)
	}
	var first uuid.UUID
	if err = f.db.QueryRow(ctx, `SELECT screening_id FROM screening.source_rescreen_targets WHERE monitor_id=$1`, monitorID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err = dispatch.RunScreening(ctx, origin.TenantID, first.String()); err != nil {
		t.Fatal(err)
	}
	state := func() map[string]any {
		t.Helper()
		raw, e := monitor.View(ctx, f.a.Principal(), uuid.MustParse(origin.ID))
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	if state()["current"] != true {
		t.Fatal("baseline not current", state())
	}
	// More than 500 independently enrolled query snapshots, one bounded SQL setup.
	exec(`WITH origins AS(INSERT INTO screening.screenings(id,tenant_id,provider,object_type,object_id,status,request_json,response_json,is_manual,idempotency_key,created_at,updated_at,completed_at)
 SELECT gen_random_uuid(),tenant_id,provider,object_type,'bulk-'||n,'no_hit',request_json,response_json,true,'manual:'||$2||':'||gen_random_uuid()::text,now(),now(),now() FROM screening.screenings CROSS JOIN generate_series(1,600)n WHERE id=$1 RETURNING id,tenant_id)
 INSERT INTO screening.source_monitors(id,tenant_id,origin_screening_id,latest_screening_id,created_by) SELECT gen_random_uuid(),tenant_id,id,id,$2 FROM origins`, origin.ID, f.a.Principal().Subject)
	exec(`INSERT INTO screening.source_monitor_datasets SELECT id,tenant_id,$1,$2 FROM screening.source_monitors WHERE id<>$3`, f.private.Owner, f.private.Dataset, monitorID)
	var fixture struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}
	if err = json.Unmarshal(f.input, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Records = append(fixture.Records, sourcedata.Record{ID: "new-designation", Name: "Future Designation", Type: "Person", SourceReference: "Fictional lifecycle fixture"})
	changed, _ := json.Marshal(fixture)
	ingest := sourceingest.Store{DB: f.db}
	generation, err := ingest.Stage(ctx, f.private, bytes.NewReader(changed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ingest.Publish(ctx, f.private, generation); err != nil {
		t.Fatal(err)
	}
	if state()["current"] != false {
		t.Fatal("old clean result still current before fanout")
	}
	// Concurrent/repeated page delivery cannot multiply receipts.
	for i := 0; i < 12; i++ {
		if _, err = monitor.FanoutPage(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var targets int
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_rescreen_targets t JOIN screening.source_publications p ON p.id=t.publication_id WHERE p.generation=$1`, generation).Scan(&targets); err != nil || targets != 601 {
		t.Fatal("missed/duplicate fanout", targets, err)
	}
	failing := sourcemonitor.Service{DB: f.db, Enqueue: sourceTestQueue{fail: true}}
	if _, err = failing.QueueOne(ctx); err == nil {
		t.Fatal("queue failure hidden")
	}
	var queued int
	f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_rescreen_targets WHERE status='queued'`).Scan(&queued)
	if queued != 0 {
		t.Fatal("queue receipt escaped rollback")
	}
	for i := 0; i < 601; i++ {
		if _, err = monitor.QueueOne(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var updated uuid.UUID
	if err = f.db.QueryRow(ctx, `SELECT screening_id FROM screening.source_rescreen_targets t JOIN screening.source_publications p ON p.id=t.publication_id WHERE t.monitor_id=$1 AND p.generation=$2`, monitorID, generation).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if err = dispatch.RunScreening(ctx, origin.TenantID, updated.String()); err != nil {
		t.Fatal(err)
	}
	result, err := store.NewScreeningRepository(f.db).GetByID(ctx, origin.TenantID, updated.String())
	if err != nil || result.Status != screening.StatusAwaitingReview {
		t.Fatal("new designation missed", result.Status, result.LastError, err)
	}
	if state()["current"] != true {
		t.Fatal("successful rescreen not current", state())
	}
	// Drain 599 further real provider executions concurrently, retaining one
	// target for the revoke/retry recovery test below. Receipt count alone does
	// not establish background completion.
	started := time.Now()
	rows, e := f.db.Query(ctx, `SELECT screening_id::text FROM screening.source_rescreen_targets WHERE status='queued' ORDER BY id LIMIT 599`)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil || len(ids) != 599 {
		t.Fatal(len(ids), e)
	}
	jobs := make(chan string, len(ids))
	for _, id := range ids {
		jobs <- id
	}
	close(jobs)
	errs := make(chan error, len(ids))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if e := dispatch.RunScreening(ctx, origin.TenantID, id); e != nil {
					errs <- e
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	t.Logf("rescreen_workload completed=%d concurrency=8 duration_ms=%d selected_dataset_records=%d", len(ids), time.Since(started).Milliseconds(), len(fixture.Records))
	old, err := store.NewScreeningRepository(f.db).GetByID(ctx, origin.TenantID, origin.ID)
	if err != nil || old.Status != screening.StatusNoHit || !bytes.Equal(old.ResponseJSON, original.ResponseJSON) {
		t.Fatal("historical clean result overwritten", err)
	}
	// Revocation at worker execution fails and cannot establish a clean result.
	exec(`DELETE FROM screening.source_grants WHERE tenant_id=$1 AND owner_scope=$2 AND dataset_id=$3`, f.a.Tenant(), f.private.Owner, f.private.Dataset)
	var other uuid.UUID
	f.db.QueryRow(ctx, `SELECT screening_id FROM screening.source_rescreen_targets WHERE status='queued' LIMIT 1`).Scan(&other)
	if err = dispatch.RunScreening(ctx, origin.TenantID, other.String()); err != nil {
		t.Fatal(err)
	}
	denied, err := store.NewScreeningRepository(f.db).GetByID(ctx, origin.TenantID, other.String())
	if err != nil || denied.Status != screening.StatusFailed {
		t.Fatal("revoked dataset produced success", denied.Status, err)
	}
	if state()["current"] != false {
		t.Fatal("revoked selection considered current")
	}
	// A new service instance recovers the persisted failure after grant repair.
	exec(`INSERT INTO screening.source_grants(tenant_id,owner_scope,dataset_id) VALUES($1,$2,$3)`, f.a.Tenant(), f.private.Owner, f.private.Dataset)
	var failedTarget, failedOrigin uuid.UUID
	if e = f.db.QueryRow(ctx, `SELECT t.id,m.origin_screening_id FROM screening.source_rescreen_targets t JOIN screening.source_monitors m ON(m.tenant_id,m.id)=(t.tenant_id,t.monitor_id) WHERE t.screening_id=$1`, other).Scan(&failedTarget, &failedOrigin); e != nil {
		t.Fatal(e)
	}
	restarted := sourcemonitor.Service{DB: f.db, Enqueue: sourceTestQueue{}}
	if e = restarted.Retry(ctx, f.a.Principal(), failedOrigin, failedTarget); e != nil {
		t.Fatal(e)
	}
	if _, e = restarted.QueueOne(ctx); e != nil {
		t.Fatal(e)
	}
	var retried string
	if e = f.db.QueryRow(ctx, `SELECT screening_id::text FROM screening.source_rescreen_targets WHERE id=$1`, failedTarget).Scan(&retried); e != nil || retried == other.String() {
		t.Fatal("retry replaced old attempt", e)
	}
	if e = dispatch.RunScreening(ctx, origin.TenantID, retried); e != nil {
		t.Fatal(e)
	}
	var complete, attempts int
	if e = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_rescreen_targets t JOIN screening.source_publications p ON p.id=t.publication_id WHERE p.generation=$1 AND t.status='completed'`, generation).Scan(&complete); e != nil || complete != 601 {
		t.Fatal("backlog incomplete", complete, e)
	}
	if e = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_rescreen_attempts WHERE target_id=$1`, failedTarget).Scan(&attempts); e != nil || attempts != 2 {
		t.Fatal("retry history lost", attempts, e)
	}
	// Long durable history is paged; cursors are bound to this exact monitor.
	exec(`WITH v AS(INSERT INTO screening.source_versions(owner_scope,dataset_id,id,parent_revision,status,actor,content_hash,record_count,published_at) SELECT $1,$2,gen_random_uuid(),0,'published','history fixture','fixture',0,now() FROM generate_series(1,80) RETURNING id),p AS(INSERT INTO screening.source_publications(owner_scope,dataset_id,generation,actor) SELECT $1,$2,id,'history fixture' FROM v RETURNING id) INSERT INTO screening.source_rescreen_targets(id,publication_id,tenant_id,monitor_id,status) SELECT gen_random_uuid(),id,$3,$4,'cancelled' FROM p`, f.private.Owner, f.private.Dataset, f.a.Tenant(), monitorID)
	seen := map[string]bool{}
	cursor := uuid.Nil
	for page := 0; page < 3; page++ {
		raw, e := monitor.ViewPage(ctx, f.a.Principal(), uuid.MustParse(origin.ID), cursor)
		if e != nil {
			t.Fatal(e)
		}
		var view struct {
			History []struct{ ID string } `json:"history"`
			Cursor  *string               `json:"history_cursor"`
		}
		if e = json.Unmarshal(raw, &view); e != nil {
			t.Fatal(e)
		}
		for _, row := range view.History {
			if seen[row.ID] {
				t.Fatal("duplicate history page", row.ID)
			}
			seen[row.ID] = true
		}
		if view.Cursor == nil {
			break
		}
		cursor = uuid.MustParse(*view.Cursor)
	}
	if len(seen) != 82 {
		t.Fatal("truncated history", len(seen))
	}
	if _, e := monitor.ViewPage(ctx, f.a.Principal(), uuid.MustParse(origin.ID), failedTarget); !errors.Is(e, sourcedata.ErrCursor) {
		t.Fatal("foreign monitor cursor accepted", e)
	}
}

func TestSourceRollbackArtifactsRetentionAndRecovery(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	ingest := sourceingest.Store{DB: f.db}
	var artifact []byte
	if err := f.db.QueryRow(ctx, `SELECT a.body FROM screening.source_artifacts a JOIN screening.source_version_artifacts v USING(owner_scope,dataset_id,sha256) WHERE v.owner_scope=$1 AND v.dataset_id=$2 AND v.generation=$3`, f.private.Owner, f.private.Dataset, f.version).Scan(&artifact); err != nil || !bytes.Equal(artifact, f.input) {
		t.Fatal("original artifact not preserved", err)
	}
	var data map[string]any
	json.Unmarshal(f.input, &data)
	records := data["records"].([]any)
	records[0].(map[string]any)["name"] = "Changed original name"
	changed, _ := json.Marshal(data)
	next, err := ingest.Stage(ctx, f.private, bytes.NewReader(changed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ingest.Publish(ctx, f.private, next); err != nil {
		t.Fatal(err)
	}
	diff, err := ingest.Diff(ctx, f.private, f.version, next, "", 1)
	if err != nil || len(diff.Changes) != 1 || diff.Changes[0].Kind != "modified" {
		t.Fatal(diff, err)
	}
	restored, err := ingest.Rollback(ctx, f.private, f.version)
	if err != nil || restored.Generation == f.version || restored.Generation == next || restored.Unchanged {
		t.Fatal(restored, err)
	}
	var count int
	f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_publications WHERE owner_scope=$1 AND dataset_id=$2`, f.private.Owner, f.private.Dataset).Scan(&count)
	if count != 3 {
		t.Fatal("rollback did not create new publication", count)
	}
	abandoned := uuid.New()
	_, err = f.db.Exec(ctx, `INSERT INTO screening.source_versions(owner_scope,dataset_id,id,parent_revision,status,actor,lease_until) VALUES($1,$2,$3,0,'staging','test',now()-interval '1 second')`, f.private.Owner, f.private.Dataset, abandoned)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := ingest.RecoverInterrupted(ctx, 1)
	if err != nil || recovered != 1 {
		t.Fatal(recovered, err)
	}
	staged, err := ingest.Stage(ctx, f.private, bytes.NewReader(changed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(ctx, `UPDATE screening.source_versions SET created_at=now()-interval '40 days' WHERE id=$1`, staged); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(ctx, `INSERT INTO screening.source_retention_policies(owner_scope,dataset_id,failed_after,staged_after,legal_hold) VALUES($1,$2,interval '7 days',interval '30 days',true)`, f.private.Owner, f.private.Dataset); err != nil {
		t.Fatal(err)
	}
	if n, e := ingest.CleanupUnpublished(ctx); e != nil || n != 0 {
		t.Fatal("legal hold ignored", n, e)
	}
	f.db.Exec(ctx, `UPDATE screening.source_retention_policies SET legal_hold=false`)
	if n, e := ingest.CleanupUnpublished(ctx); e != nil || n == 0 {
		t.Fatal("expired staging not cleaned", n, e)
	}
	if _, e := ingest.Publish(ctx, f.private, staged); !errors.Is(e, sourcedata.ErrConflict) {
		t.Fatal("cleaned stage was publishable", e)
	}
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_records WHERE owner_scope=$1 AND dataset_id=$2 AND generation=ANY($3)`, f.private.Owner, f.private.Dataset, []uuid.UUID{f.version, next, restored.Generation}).Scan(&count); err != nil || count == 0 {
		t.Fatal("published evidence removed", count, err)
	}
	// Failed raw downloads without a staged generation also need bounded GC.
	// Legal holds and review references win; two concurrent cleaners cannot
	// delete the same item or erase a retained publication's artifact.
	for i := 0; i < 3; i++ {
		hash := fmt.Sprintf("%064d", i+1)
		if _, e := f.db.Exec(ctx, `INSERT INTO screening.source_artifacts(owner_scope,dataset_id,sha256,body,media_type,created_at) VALUES($1,$2,$3,'expired fictional download','text/plain',now()-interval '40 days')`, f.private.Owner, f.private.Dataset, hash); e != nil {
			t.Fatal(e)
		}
	}
	held := fmt.Sprintf("%064d", 1)
	if _, e := f.db.Exec(ctx, `INSERT INTO screening.source_retention_holds(id,owner_scope,dataset_id,reference_id,sha256,reason) VALUES($1,$2,$3,$4,$5,'fixture-review')`, uuid.New(), f.private.Owner, f.private.Dataset, uuid.New(), held); e != nil {
		t.Fatal(e)
	}
	var cleaned atomic.Int64
	var cleaners sync.WaitGroup
	for i := 0; i < 2; i++ {
		cleaners.Add(1)
		go func() {
			defer cleaners.Done()
			n, e := ingest.CleanupArtifacts(ctx)
			if e != nil {
				t.Error(e)
			}
			cleaned.Add(n)
		}()
	}
	cleaners.Wait()
	if cleaned.Load() != 2 {
		t.Fatal("artifact cleanup lost a hold or duplicated deletes", cleaned.Load())
	}
	if e := f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_artifacts WHERE owner_scope=$1 AND dataset_id=$2 AND sha256=$3`, f.private.Owner, f.private.Dataset, held).Scan(&count); e != nil || count != 1 {
		t.Fatal("held raw evidence lost", count, e)
	}
}
