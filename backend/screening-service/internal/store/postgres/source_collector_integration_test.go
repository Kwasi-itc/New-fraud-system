package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
)

type sourceRoundTrip func(*http.Request) (*http.Response, error)

func (fn sourceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestSourceCollectorFencesConditionalChecksAndQuarantine(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	key := sourceingest.Key{Dataset: "ofac-fixture"}
	spec, _ := sourceconnectors.Lookup("ofac-sdn")
	pipeline := sourcecollector.Pipeline{DB: f.db}
	if err := pipeline.Register(ctx, key, spec); err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile("../../../testdata/source-fixtures/ofac-sdn.xml")
	if err != nil {
		t.Fatal(err)
	}
	status := 200
	requireFullFetch := false
	calls := 0
	client := &http.Client{Transport: sourceRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if requireFullFetch && r.Header.Get("If-None-Match") != "" {
			t.Fatal("normalizer upgrade incorrectly used 304 cache")
		}
		if status == 304 && r.Header.Get("If-None-Match") != "fictional-v1" {
			t.Fatal("conditional metadata lost")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Etag": []string{"fictional-v1"}}, Body: io.NopCloser(bytes.NewReader(input)), ContentLength: int64(len(input)), Request: r}, nil
	})}
	collector := sourcecollector.Collector{DB: f.db, Dir: t.TempDir(), Fetcher: sourceconnectors.Fetcher{Client: client}}
	if work, e := collector.RunOne(ctx); e != nil || work || calls != 0 {
		t.Fatal("unreviewed source ran", work, calls, e)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := f.db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	// Synthetic-only approval exercises technical gates; not a real legal review.
	exec(`UPDATE screening.source_connections SET enabled=true,rights_state='approved',rights_review_reference='synthetic fixture only' WHERE dataset_id=$1`, key.Dataset)
	exec(`INSERT INTO screening.source_grants(tenant_id,owner_scope,dataset_id) VALUES($1,$2,$3)`, f.a.Tenant(), key.Owner, key.Dataset)
	if work, e := collector.RunOne(ctx); e != nil || !work {
		t.Fatal(work, e)
	}
	var generation uuid.UUID
	var ready bool
	if err = f.db.QueryRow(ctx, `SELECT d.active_generation,r.ready FROM screening.source_datasets d JOIN screening.source_readiness_v1 r ON(r.owner_scope,r.dataset_id)=(d.owner_scope,d.id) WHERE d.owner_scope=$1 AND d.id=$2`, key.Owner, key.Dataset).Scan(&generation, &ready); err != nil || !ready {
		t.Fatal(generation, ready, err)
	}
	provider := internalprovider.Provider{DB: f.db}
	req := internalRequest("Ama Fixture Mensori", "Person", sourcedata.DatasetRef{Scope: "global", ID: key.Dataset})
	result, err := provider.Search(ctx, f.a, req)
	if err != nil || len(result.Matches) != 1 {
		t.Fatal(len(result.Matches), err)
	}
	var identifiers int
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_identifiers WHERE dataset_id=$1 AND original='0012345' AND country='gh'`, key.Dataset).Scan(&identifiers); err != nil || identifiers != 1 {
		t.Fatal("typed identifier lost", identifiers, err)
	}
	status = 304
	exec(`UPDATE screening.source_connections SET next_check_at=now()`)
	if work, e := collector.RunOne(ctx); e != nil || !work {
		t.Fatal(work, e)
	}
	var publications int
	f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_publications WHERE dataset_id=$1`, key.Dataset).Scan(&publications)
	if publications != 1 {
		t.Fatal("304 fabricated publication", publications)
	}
	// New normalization rules require a new projection even for identical XML.
	// A conditional response must not keep the old index indefinitely current.
	exec(`UPDATE screening.source_versions SET normalizer='nfkc-casefold-v1' WHERE id=$1`, generation)
	exec(`UPDATE screening.source_connections SET next_check_at=now()`)
	status = 200
	requireFullFetch = true
	if work, e := collector.RunOne(ctx); e != nil || !work {
		t.Fatal(work, e)
	}
	var upgraded uuid.UUID
	if err = f.db.QueryRow(ctx, `SELECT active_generation FROM screening.source_datasets WHERE id=$1`, key.Dataset).Scan(&upgraded); err != nil || upgraded == generation {
		t.Fatal("normalizer upgrade stayed on old generation", upgraded, err)
	}
	generation = upgraded
	requireFullFetch = false
	// Truncated new data cannot refresh last success or preserve a false clean
	// currentness assertion merely because the previous check was recent.
	status = 200
	input = input[:len(input)/2]
	exec(`UPDATE screening.source_connections SET next_check_at=now()`)
	if work, e := collector.RunOne(ctx); e != nil || !work {
		t.Fatal(work, e)
	}
	if err = f.db.QueryRow(ctx, `SELECT ready FROM screening.source_readiness_v1 WHERE dataset_id=$1`, key.Dataset).Scan(&ready); err != nil || ready {
		t.Fatal("quarantine still ready", ready, err)
	}
	if _, e := provider.Search(ctx, f.a, req); !errors.Is(e, sourcedata.ErrNotReady) {
		t.Fatal("quarantine produced successful search", e)
	}
	var after uuid.UUID
	f.db.QueryRow(ctx, `SELECT active_generation FROM screening.source_datasets WHERE id=$1`, key.Dataset).Scan(&after)
	if after != generation {
		t.Fatal("bad source replaced last good generation")
	}
	var retained bool
	if err = f.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.source_import_runs r JOIN screening.source_artifacts a ON(a.owner_scope,a.dataset_id,a.sha256)=(r.owner_scope,r.dataset_id,r.artifact_sha256) WHERE r.dataset_id=$1 AND r.status='quarantined' AND a.body=$2)`, key.Dataset, input).Scan(&retained); err != nil || !retained {
		t.Fatal("quarantined raw artifact lost", retained, err)
	}
	// A new lease fences the old publisher, and cannot adopt its staged data.
	exec(`UPDATE screening.source_connections SET enabled=true,next_check_at=now()`)
	first, e := collector.Claim(ctx)
	if e != nil || first == nil {
		t.Fatal(first, e)
	}
	stage, e := os.CreateTemp(t.TempDir(), "snapshot-")
	if e != nil {
		t.Fatal(e)
	}
	full, e := os.ReadFile("../../../testdata/source-fixtures/ofac-sdn.xml")
	if e != nil {
		t.Fatal(e)
	}
	stage.Write(full)
	stage.Close()
	staged, _, e := pipeline.StageSnapshot(ctx, key, spec, stage.Name())
	if e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE screening.source_import_runs SET status='staged',generation=$2 WHERE id=$1`, first.Run, staged)
	exec(`UPDATE screening.source_connections SET lease_until=now()-interval '1 second'`)
	second, e := collector.Claim(ctx)
	if e != nil || second == nil || second.Fence <= first.Fence {
		t.Fatal(second, e)
	}
	if _, e = collector.PublishStaged(ctx, *first, staged); !errors.Is(e, sourcedata.ErrConflict) {
		t.Fatal("expired publisher not fenced", e)
	}
	if _, e = collector.PublishStaged(ctx, *second, staged); !errors.Is(e, sourcedata.ErrConflict) {
		t.Fatal("new lease adopted another run's stage", e)
	}
	var sourceVersion struct {
		Key    string `json:"source_key"`
		Parser string `json:"parser_version"`
	}
	var metadata []byte
	if e = f.db.QueryRow(ctx, `SELECT jsonb_build_object('source_key',source_key,'parser_version',parser_version) FROM screening.source_versions WHERE id=$1`, generation).Scan(&metadata); e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(metadata, &sourceVersion)
	if sourceVersion.Key != spec.Key || sourceVersion.Parser != spec.Parser {
		t.Fatal(sourceVersion)
	}
}
