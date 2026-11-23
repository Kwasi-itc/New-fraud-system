package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
)

func TestApprovedLocalSnapshotPublicationIsAtomicAuditedAndSearchable(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	key := sourceingest.Key{Dataset: "judiciary-snapshot-fixture"}
	store := sourceingest.Store{DB: f.db}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	if e := store.Register(ctx, key, "Fictional judiciary snapshot", []uuid.UUID{f.a.Tenant()}); e != nil {
		t.Fatal(e)
	}
	exec(`INSERT INTO screening.source_connections(owner_scope,dataset_id,connector,config,enabled) VALUES($1,$2,'fictional-judiciary','{"parser":"fictional-json-v1"}',true)`, key.Owner, key.Dataset)
	input := func(name string) []byte {
		b, e := json.Marshal(struct {
			Version int                 `json:"version"`
			Source  string              `json:"source"`
			Parser  string              `json:"parser"`
			Records []sourcedata.Record `json:"records"`
		}{2, "fictional-judiciary", "fictional-json-v1", []sourcedata.Record{{ID: "fixture-001", Name: name, Type: "Person", SourceReference: "Authored fixture, not a real public office claim"}}})
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	stage := func(name string) uuid.UUID {
		t.Helper()
		gen, e := store.Stage(ctx, key, bytes.NewReader(input(name)))
		if e != nil {
			t.Fatal(e)
		}
		return gen
	}
	state := func() (int, bool, int, int) {
		t.Helper()
		var records, receipts, audits int
		var ready bool
		e := f.db.QueryRow(ctx, `SELECT coalesce(v.record_count,0),coalesce(rd.ready,false),(SELECT count(*) FROM screening.source_import_runs WHERE dataset_id=$2),(SELECT count(*) FROM screening.source_lifecycle_audit WHERE dataset_id=$2 AND action='snapshot-accepted') FROM screening.source_datasets d LEFT JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation) LEFT JOIN screening.source_readiness_v1 rd ON(rd.owner_scope,rd.dataset_id)=(d.owner_scope,d.id) WHERE d.owner_scope=$1 AND d.id=$2`, key.Owner, key.Dataset).Scan(&records, &ready, &receipts, &audits)
		if e != nil {
			t.Fatal(e)
		}
		return records, ready, receipts, audits
	}
	gen := stage("Ama Judicial Fixture")
	if _, e := store.PublishSnapshot(ctx, key, gen, "fixture operator acceptance"); !errors.Is(e, sourcedata.ErrPublish) {
		t.Fatal("unreviewed source accepted", e)
	}
	if n, ready, r, a := state(); n != 0 || ready || r != 0 || a != 0 {
		t.Fatal("rejected intake mutated active source", n, ready, r, a)
	}
	// Approval is fictional, exercising the technical gate only.
	exec(`UPDATE screening.source_connections SET rights_state='approved',rights_review_reference='fictional test source only' WHERE dataset_id=$1`, key.Dataset)
	// Force a late audit failure: pointer, readiness and receipt must roll back together.
	exec(`CREATE FUNCTION screening.fail_snapshot_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic late audit failure'; END $$; CREATE TRIGGER fail_snapshot_test BEFORE INSERT ON screening.source_lifecycle_audit FOR EACH ROW WHEN(NEW.action='snapshot-accepted') EXECUTE FUNCTION screening.fail_snapshot_test()`)
	if _, e := store.PublishSnapshot(ctx, key, gen, "fixture operator acceptance"); e == nil {
		t.Fatal("late failure accepted")
	}
	if n, ready, r, a := state(); n != 0 || ready || r != 0 || a != 0 {
		t.Fatal("partial publication after audit failure", n, ready, r, a)
	}
	exec(`DROP TRIGGER fail_snapshot_test ON screening.source_lifecycle_audit; DROP FUNCTION screening.fail_snapshot_test()`)
	publication, e := store.PublishSnapshot(ctx, key, gen, "fixture operator acceptance")
	if e != nil || publication.Generation != gen || publication.Unchanged {
		t.Fatal(publication, e)
	}
	if n, ready, r, a := state(); n != 1 || !ready || r != 1 || a != 1 {
		t.Fatal("missing atomic acceptance state", n, ready, r, a)
	}
	var paused, annotated bool
	if e = f.db.QueryRow(ctx, `SELECT NOT enabled AND lease_token IS NULL,config->>'intake_mode'='reviewed_snapshot' AND config->'snapshot_intake'->>'external_fetch'='false' FROM screening.source_connections WHERE dataset_id=$1`, key.Dataset).Scan(&paused, &annotated); e != nil || !paused || !annotated {
		t.Fatal("local snapshot masqueraded as live collection", paused, annotated, e)
	}
	request := internalRequest("Ama Judicial Fixture", "Person", sourcedata.DatasetRef{Scope: "global", ID: key.Dataset})
	result, e := (internalprovider.Provider{DB: f.db}).Search(ctx, f.a, request)
	if e != nil || result.Partial || len(result.Matches) != 1 {
		t.Fatal("published snapshot not searchable", result, e)
	}
	if _, e = (internalprovider.Provider{DB: f.db}).Search(ctx, f.b, request); e == nil {
		t.Fatal("snapshot bypassed tenant grant")
	}
	duplicate := stage("Ama Judicial Fixture")
	if publication, e = store.PublishSnapshot(ctx, key, duplicate, "repeat local acceptance, not external fetch"); e != nil || !publication.Unchanged || publication.Generation != gen {
		t.Fatal("repeat created replacement publication", publication, e)
	}
	if n, ready, r, a := state(); n != 1 || !ready || r != 2 || a != 2 {
		t.Fatal(n, ready, r, a)
	}
	next := stage("Changed Judicial Fixture")
	exec(`UPDATE screening.source_connections SET rights_state='blocked' WHERE dataset_id=$1`, key.Dataset)
	if _, e = store.PublishSnapshot(ctx, key, next, "fixture acceptance"); !errors.Is(e, sourcedata.ErrPublish) {
		t.Fatal("revoked rights ignored", e)
	}
	if n, _, r, a := state(); n != 1 || r != 2 || a != 2 {
		t.Fatal("rejected replacement mutated publication", n, r, a)
	}
	exec(`UPDATE screening.source_connections SET rights_state='approved' WHERE dataset_id=$1`, key.Dataset)
	// Ordinary manual publication keeps its existing quarantine behavior.
	if _, e = store.Publish(ctx, key, next); e != nil {
		t.Fatal(e)
	}
	if _, ready, _, _ := state(); ready {
		t.Fatal("ordinary manual publication acquired freshness")
	}
}
