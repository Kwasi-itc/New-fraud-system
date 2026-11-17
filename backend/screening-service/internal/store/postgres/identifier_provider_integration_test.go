package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"testing"
)

func TestTypedIdentifiersRetrieveWithoutNameOverlapAndPreserveScope(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	p := internalprovider.Provider{DB: f.db}
	record := sourcedata.Record{ID: "id-only", Name: "Fictional Qzvth Entity", Type: "Person", SourceReference: "fictional typed ID fixture", Identifiers: []sourcedata.Identifier{{Type: "passport", Value: "001-Ab/09", Country: "GH"}}}
	raw, _ := json.Marshal(struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Parser  string              `json:"parser"`
		Records []sourcedata.Record `json:"records"`
	}{2, "tenant-upload", "controlled-source-v2", []sourcedata.Record{record}})
	ingest := sourceingest.Store{DB: f.db}
	generation, e := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, f.private, generation); e != nil {
		t.Fatal(e)
	}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}
	config, _ := json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.IdentifierProfile, Threshold: 85, Datasets: []sourcedata.DatasetRef{ref}})
	request := internalRequest("Unrelated Latin spelling", "Person", ref)
	request.ProviderConfig = config
	request.Queries[0].Identifiers = []sourcedata.Identifier{{Type: "passport", Value: "001-Ab/09", Country: "gh"}}
	result, e := p.Search(ctx, f.a, request)
	if e != nil || result.Partial || len(result.Matches) != 1 || result.Matches[0].Score != 1 {
		t.Fatal("exact ID retrieval", result, e)
	}
	var evidence struct {
		Matching struct {
			Strategy string `json:"strategy"`
		} `json:"matching"`
		Ref sourcedata.CandidateRef `json:"provenance"`
	}
	json.Unmarshal(result.Matches[0].Payload, &evidence)
	if evidence.Matching.Strategy != "exact_identifier_with_name_evidence" || evidence.Ref.Generation != generation {
		t.Fatal(evidence)
	}
	for _, id := range []sourcedata.Identifier{{Type: "passport", Value: "1-Ab/09", Country: "gh"}, {Type: "passport", Value: "001-ab/09", Country: "gh"}, {Type: "passport", Value: "001-Ab/09", Country: "gb"}, {Type: "national_id", Value: "001-Ab/09", Country: "gh"}} {
		request.Queries[0].Identifiers = []sourcedata.Identifier{id}
		result, e = p.Search(ctx, f.a, request)
		if e != nil || len(result.Matches) != 0 {
			t.Fatal("identifier coerced or namespace ignored", id, e)
		}
	}
	request.Queries[0].Identifiers = []sourcedata.Identifier{{Type: "passport", Value: "001-Ab/09", Country: "gh"}}
	if _, e = p.Search(ctx, f.b, request); !errors.Is(e, sourcedata.ErrDataset) {
		t.Fatal("foreign private source", e)
	}
	legacy, _ := json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.FuzzyProfile, Threshold: 85, Datasets: []sourcedata.DatasetRef{ref}})
	request.ProviderConfig = legacy
	if _, e = p.Search(ctx, f.a, request); !errors.Is(e, sourcedata.ErrInput) {
		t.Fatal("frozen profile silently ignored typed IDs", e)
	}
	request.ProviderConfig = config
	request.Queries = []screening.SearchQuery{{Name: "Unrelated", Type: "Organization", Identifiers: []sourcedata.Identifier{{Type: "passport", Value: "001-Ab/09", Country: "gh"}}}}
	if result, e = p.Search(ctx, f.a, request); e != nil || len(result.Matches) != 0 {
		t.Fatal("entity type ignored", result, e)
	}
}
