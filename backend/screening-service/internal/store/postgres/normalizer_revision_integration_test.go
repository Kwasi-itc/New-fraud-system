package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"testing"
)

func TestNormalizerUpgradeRequiresNewPublicationAndKeepsHistoricalEvidence(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	ingest := sourceingest.Store{DB: f.db}
	p := internalprovider.Provider{DB: f.db}
	name := "\u1c89 Fictional Name" // Unicode-16 Cyrillic Tje; Unicode-15 did not fold it.
	if sourcedata.Normalize(name) == "\u1c89 fictional name" {
		t.Fatal("fixture must exercise a changed Unicode table")
	}
	raw, _ := json.Marshal(struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}{1, "controlled-fixture", []sourcedata.Record{{ID: "unicode-revision", Name: name, Type: "Person", SourceReference: "fictional Unicode normalization fixture"}}})
	old, e := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, f.private, old); e != nil {
		t.Fatal(e)
	}
	// Simulate a retained Unicode-15 projection, without changing its source data.
	if _, e = f.db.Exec(ctx, `UPDATE screening.source_versions SET normalizer='nfkc-casefold-v1' WHERE id=$1;`, old); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(ctx, `UPDATE screening.source_names SET normalized=$2 WHERE generation=$1`, old, "\u1c89 fictional name"); e != nil {
		t.Fatal(e)
	}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}
	request := internalRequest(name, "Person", ref)
	if _, e = p.Search(ctx, f.a, request); !errors.Is(e, sourcedata.ErrNotReady) {
		t.Fatal("old projection interpreted with new Unicode tables", e)
	}
	if _, e = p.Enrich(ctx, f.a, sourcedata.CandidateRef{Dataset: ref, Generation: old, RecordID: "unicode-revision"}); e != nil {
		t.Fatal("old retained source became unreadable", e)
	}
	next, e := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	pub, e := ingest.Publish(ctx, f.private, next)
	if e != nil || pub.Unchanged || pub.Generation == old {
		t.Fatal("same content hid normalizer upgrade", pub, e)
	}
	result, e := p.Search(ctx, f.a, request)
	if e != nil || result.Partial || len(result.Matches) != 1 {
		t.Fatal("new Unicode projection did not match", result, e)
	}
	var versions int
	if e = f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_versions WHERE id=ANY($1) AND status='published'`, []any{old, next}).Scan(&versions); e != nil || versions != 2 {
		t.Fatal("normalizer upgrade rewrote history", versions, e)
	}
	again, e := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if pub, e = ingest.Publish(ctx, f.private, again); e != nil || !pub.Unchanged || pub.Generation != next {
		t.Fatal("current normalizer not idempotent", pub, e)
	}
}
