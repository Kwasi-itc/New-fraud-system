package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	legacy "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerregistry"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
)

func TestInternalProviderFuzzyEvidenceAndLifecycle(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	p := internalprovider.Provider{DB: f.db}
	ingest := sourceingest.Store{DB: f.db}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}
	req := internalRequest("Ama Testpreson", "Person", ref)
	req.ProviderConfig, _ = json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.FuzzyProfile, Threshold: 85, Datasets: []sourcedata.DatasetRef{ref}})
	result, err := p.Search(ctx, f.a, req)
	if err != nil || len(result.Matches) != 1 {
		t.Fatal("typo retrieval", result, err)
	}
	var payload struct {
		Profile    string                  `json:"profile"`
		Properties map[string][]string     `json:"properties"`
		Provenance sourcedata.CandidateRef `json:"provenance"`
	}
	if json.Unmarshal(result.Matches[0].Payload, &payload) != nil || payload.Profile != sourcedata.FuzzyProfile || len(payload.Properties["comparisonValues"]) != 1 || payload.Provenance.Generation != f.version {
		t.Fatal("explanation/provenance missing", payload)
	}
	if _, err = p.Search(ctx, f.b, req); !errors.Is(err, sourcedata.ErrDataset) {
		t.Fatal("fuzzy tenant isolation", err)
	}
	// Updating one stable source ID preserves entity identity; removing an alias
	// affects only new searches, while prior-generation enrichment remains intact.
	var envelope struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}
	json.Unmarshal(f.input, &envelope)
	envelope.Records = envelope.Records[:1]
	envelope.Records[0].Name = "Ama Changedperson"
	envelope.Records[0].Aliases = []string{"Changed Fixture Alias"}
	envelope.Records = append(envelope.Records, sourcedata.Record{ID: "new-person", Name: "Elin Addedperson", Type: "Person", SourceReference: "Controlled new record"})
	raw, _ := json.Marshal(envelope)
	gen, err := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ingest.Publish(ctx, f.private, gen); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name, kind string
		hits       int
	}{{"Ama Fixture Alias", "Person", 0}, {"Fixture Payments", "Organization", 0}, {"Changed Fixture Alias", "Person", 1}, {"Elin Addedperson", "Person", 1}}
	for _, tc := range checks {
		got, e := p.Search(ctx, f.a, internalRequest(tc.name, tc.kind, ref))
		if e != nil || len(got.Matches) != tc.hits {
			t.Fatal(tc, got, e)
		}
		if tc.name == "Changed Fixture Alias" && got.Matches[0].EntityID != result.Matches[0].EntityID {
			t.Fatal("source identity changed with name")
		}
	}
	if _, err = p.Enrich(ctx, f.a, payload.Provenance); err != nil {
		t.Fatal("historical evidence lost", err)
	}
	var versions, publications int
	if err = f.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND status='published'),(SELECT count(*) FROM screening.source_publications WHERE owner_scope=$1 AND dataset_id=$2)`, f.private.Owner, f.private.Dataset).Scan(&versions, &publications); err != nil || versions != 2 || publications != 2 {
		t.Fatal("version audit", versions, publications, err)
	}
	staged, err := ingest.Stage(ctx, f.private, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(ctx, `UPDATE screening.source_datasets SET active_generation=$3 WHERE owner_scope=$1 AND id=$2`, f.private.Owner, f.private.Dataset, staged); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Search(ctx, f.a, internalRequest("Changed Fixture Alias", "Person", ref)); !errors.Is(err, sourcedata.ErrNotReady) {
		t.Fatal("staged pointer treated as published", err)
	}
	if _, err = f.db.Exec(ctx, `UPDATE screening.source_datasets SET active_generation=$3 WHERE owner_scope=$1 AND id=$2`, f.private.Owner, f.private.Dataset, gen); err != nil {
		t.Fatal(err)
	}
}

func TestInternalProviderFuzzyOverflowIsPartial(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	ingest := sourceingest.Store{DB: f.db}
	key := sourceingest.Key{Owner: f.a.Tenant(), Dataset: "overflow"}
	if err := ingest.Register(ctx, key, "Controlled overflow", []uuid.UUID{f.a.Tenant()}); err != nil {
		t.Fatal(err)
	}
	records := []sourcedata.Record{}
	for i := 0; i < internalprovider.CandidateBound+5; i++ {
		records = append(records, sourcedata.Record{ID: fmt.Sprint(i), Name: fmt.Sprintf("Shared Fixture Name %04d", i), Type: "Person", SourceReference: "Controlled overflow"})
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "source": "controlled-fixture", "records": records})
	gen, err := ingest.Stage(ctx, key, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ingest.Publish(ctx, key, gen); err != nil {
		t.Fatal(err)
	}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: key.Dataset}
	req := internalRequest("Shared Fixture Name 9999", "Person", ref)
	req.ProviderConfig, _ = json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.FuzzyProfile, Threshold: 100, Datasets: []sourcedata.DatasetRef{ref}})
	result, err := (internalprovider.Provider{DB: f.db}).Search(ctx, f.a, req)
	if err != nil || !result.Partial || len(result.Matches) != 0 {
		t.Fatal("overflow became a complete no-hit", result, err)
	}
	if _, err = (internalprovider.Provider{DB: f.db}).Search(ctx, provideraccess.Scope{}, req); err == nil {
		t.Fatal("scope bypass")
	}
}

func TestInternalProviderMaintenanceDoesNotInventPrivateJobAuthority(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	repo := store.NewDatasetUpdateJobRepository(f.db)
	now := time.Now().UTC()
	item, err := repo.Create(ctx, screening.DatasetUpdateJob{ID: uuid.NewString(), TenantID: f.a.Tenant().String(), Provider: sourcedata.Provider, JobType: "dataset_delta_rescreen", Status: screening.DatasetUpdateJobStatusPending, ResultJSON: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	registry := providerregistry.New(f.db, legacy.NewHTTPClient("", nil, time.Second, legacy.OpenSanctionsConfig{}))
	worker := service.NewDatasetUpdateWorkerService(store.NewTransactionManager(f.db), deliveryTestClock{}, repo, store.NewContinuousConfigRepository(f.db), store.NewMonitoredObjectRepository(f.db), registry, nil)
	if err = worker.RunJob(ctx, item.TenantID, item.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || saved.Status != screening.DatasetUpdateJobStatusFailed {
		t.Fatal("tenant argument became private maintenance authority", saved.Status, err)
	}
	// This is an intentional deny invariant, not a claim that private automatic
	// rescreening is implemented. OPS-02 still owns subscription authority/fanout.
}
