package providerproduct_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func productDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable SCREENING_TEST_DATABASE_URL with CREATE DATABASE required")
	}
	ctx := context.Background()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal("invalid test DSN")
	}
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	name := "provider_product_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	cfg = cfg.Copy()
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 8
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		_, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		admin.Close()
		if e != nil {
			t.Error(e)
		}
	})
	return db
}

func publish(t *testing.T, db *pgxpool.Pool, key sourceingest.Key, record sourcedata.Record) uuid.UUID {
	t.Helper()
	if key.Owner == uuid.Nil {
		if _, e := db.Exec(context.Background(), `INSERT INTO screening.source_connections(owner_scope,dataset_id,connector,rights_state,rights_review_reference) VALUES($1,$2,'tenant-upload','approved','Authored fictional test fixture only') ON CONFLICT(owner_scope,dataset_id) DO NOTHING`, key.Owner, key.Dataset); e != nil {
			t.Fatal(e)
		}
	}
	raw, e := json.Marshal(struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}{2, "tenant-upload", []sourcedata.Record{record}})
	if e != nil {
		t.Fatal(e)
	}
	store := sourceingest.Store{DB: db}
	gen, e := store.Stage(context.Background(), key, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Publish(context.Background(), key, gen); e != nil {
		t.Fatal(e)
	}
	if key.Owner == uuid.Nil {
		if _, e = db.Exec(context.Background(), `UPDATE screening.source_connections SET last_error='',last_success_at=now(),verified_generation=$3 WHERE owner_scope=$1 AND dataset_id=$2`, key.Owner, key.Dataset, gen); e != nil {
			t.Fatal(e)
		}
	}
	return gen
}

func TestStandaloneCatalogAndAuthenticatedFiveOperations(t *testing.T) {
	db := productDB(t)
	ctx := context.Background()
	if e := providerproduct.Bootstrap(ctx, db); e != nil {
		t.Fatal(e)
	}
	if e := providerproduct.Bootstrap(ctx, db); e != nil {
		t.Fatal("repeat bootstrap", e)
	}
	var foreign bool
	if e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname IN('core','identity','case_manager')) OR to_regclass('screening.screenings') IS NOT NULL OR to_regclass('public.river_job') IS NOT NULL OR to_regclass('screening.source_publication_delivery') IS NOT NULL`).Scan(&foreign); e != nil || foreign {
		t.Fatal("standalone created fraud workflow dependencies", foreign, e)
	}
	a, b := uuid.New(), uuid.New()
	store := sourceingest.Store{DB: db}
	global := sourceingest.Key{Dataset: "public-demo"}
	own := sourceingest.Key{Owner: a, Dataset: "private-demo"}
	other := sourceingest.Key{Owner: b, Dataset: "private-demo"}
	for _, key := range []sourceingest.Key{global, own, other} {
		if e := store.Register(ctx, key, "Fictional product fixture", nil); e != nil {
			t.Fatal(e)
		}
	}
	record := sourcedata.Record{ID: "fixture-1", Name: "Ama Testperson", Aliases: []string{"Ama Fixture Alias"}, Type: "Person", SourceReference: "Fictional acceptance fixture", OriginalXML: "PRIVATE_RAW_XML_MUST_NOT_LEAK", Identifiers: []sourcedata.Identifier{{Type: "passport", Value: "001-Ab/09", Country: "GH"}}}
	globalGen := publish(t, db, global, record)
	ownGen := publish(t, db, own, record)
	otherRecord := record
	otherRecord.Name = "Foreign Namespace Secret Person"
	otherGen := publish(t, db, other, otherRecord)
	creds := providerproduct.Credentials{DB: db}
	create := func(namespace uuid.UUID, ops []string, budget int) (uuid.UUID, uuid.UUID, string) {
		t.Helper()
		client, e := creds.Create(ctx, providerproduct.ClientInput{Name: "Fictional API client", Namespace: namespace, Operations: ops, RequestsPerMinute: budget, ExpiresAt: time.Now().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		var token string
		key, e := creds.Issue(ctx, client, time.Hour, func(v string) error { token = v; return nil })
		if e != nil {
			t.Fatal(e)
		}
		return client, key, token
	}
	ops := []string{"search", "enrich", "catalog", "freshness", "delta"}
	client, key, token := create(a, ops, 1000)
	otherClient, _, otherToken := create(b, ops, 1000)
	for _, pair := range []struct {
		client uuid.UUID
		ref    sourcedata.DatasetRef
	}{{client, sourcedata.DatasetRef{Scope: "global", ID: global.Dataset}}, {client, sourcedata.DatasetRef{Scope: "tenant", ID: own.Dataset}}, {otherClient, sourcedata.DatasetRef{Scope: "global", ID: global.Dataset}}, {otherClient, sourcedata.DatasetRef{Scope: "tenant", ID: other.Dataset}}} {
		if e := creds.Grant(ctx, pair.client, pair.ref, true); e != nil {
			t.Fatal(e)
		}
	}
	login := "provider_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	password := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	if e := providerproduct.ProvisionLogin(ctx, db, login, password, "screening_provider_reader"); e != nil {
		t.Fatal(e)
	}
	cfg := db.Config()
	cfg.ConnConfig.User = login
	cfg.ConnConfig.Password = password
	reader, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	t.Cleanup(func() {
		if _, e := db.Exec(context.Background(), "DROP OWNED BY "+pgx.Identifier{login}.Sanitize()+"; DROP ROLE "+pgx.Identifier{login}.Sanitize()); e != nil {
			t.Error(e)
		}
	})
	// Cleanup must close this pool before its LOGIN role is removed.
	t.Cleanup(reader.Close)
	if _, e = reader.Exec(ctx, `UPDATE provider_api.clients SET enabled=true`); e == nil {
		t.Fatal("API login changed client permissions")
	}
	if _, e = reader.Exec(ctx, `UPDATE screening.source_datasets SET enabled=false`); e == nil {
		t.Fatal("API login changed sources")
	}
	var logs bytes.Buffer
	handler, e := (providerproduct.Server{DB: reader, Provider: internalprovider.Provider{DB: reader}, Access: providerproduct.Credentials{DB: reader}, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Timeout: 2 * time.Second, MaxConcurrent: 8}).Handler()
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	request := func(method, path, key string, body any, want int) []byte {
		t.Helper()
		var input io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			input = bytes.NewReader(raw)
		}
		req, e := http.NewRequest(method, server.URL+path, input)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s status %d expected %d, body %s", method, path, res.StatusCode, want, raw)
		}
		return raw
	}
	request("GET", "/healthz", "", nil, 200)
	request("GET", "/readyz", "", nil, 200)
	request("GET", "/docs", "", nil, 200)
	request("GET", "/openapi.yaml", "", nil, 200)
	request("GET", "/v1/datasets", "", nil, 401)
	request("GET", "/v1/datasets", token+"wrong", nil, 401)
	catalog := request("GET", "/v1/datasets", token, nil, 200)
	if bytes.Contains(catalog, []byte("Foreign Namespace")) || bytes.Contains(catalog, []byte(otherGen.String())) {
		t.Fatal("foreign catalog leaked")
	}
	in := providerproduct.SearchInput{Profile: sourcedata.FuzzyProfile, Threshold: 85, Datasets: []sourcedata.DatasetRef{{Scope: "global", ID: global.Dataset}}, Queries: []screening.SearchQuery{{Name: "Ama Testpreson", Type: "Person"}}, Limit: 10}
	result := request("POST", "/v1/search", token, in, 200)
	var found struct {
		Partial   bool                      `json:"partial"`
		Matches   []screening.ProviderMatch `json:"matches"`
		Reference string                    `json:"provider_reference"`
	}
	if json.Unmarshal(result, &found) != nil || found.Partial || len(found.Matches) != 1 || found.Reference == "" {
		t.Fatal("actual synchronous match", string(result))
	}
	if bytes.Contains(result, []byte(record.OriginalXML)) {
		t.Fatal("raw XML leaked")
	}
	var payload struct {
		Ref sourcedata.CandidateRef `json:"provenance"`
	}
	if json.Unmarshal(found.Matches[0].Payload, &payload) != nil || payload.Ref.Generation != globalGen {
		t.Fatal("source attribution lost")
	}
	details := request("POST", "/v1/enrich", token, payload.Ref, 200)
	if !bytes.Contains(details, []byte("001-Ab/09")) || bytes.Contains(details, []byte(record.OriginalXML)) {
		t.Fatal("unsafe or missing enrichment")
	}
	request("POST", "/v1/enrich", token, sourcedata.CandidateRef{Dataset: sourcedata.DatasetRef{Scope: "tenant", ID: own.Dataset}, Generation: otherGen, RecordID: record.ID}, 410)
	request("GET", "/v1/freshness?dataset=global:public-demo", token, nil, 200)
	delta := request("GET", "/v1/deltas?dataset=global:public-demo&limit=1", token, nil, 200)
	var page struct {
		Next string `json:"next_cursor"`
	}
	if json.Unmarshal(delta, &page) != nil || page.Next == "" {
		t.Fatal("delta cursor missing")
	}
	request("GET", "/v1/deltas?dataset=global:public-demo&cursor="+page.Next, otherToken, nil, 400)
	in.Profile = sourcedata.IdentifierProfile
	in.Queries = []screening.SearchQuery{{Name: "Unrelated spelling", Type: "Person", Identifiers: []sourcedata.Identifier{{Type: "passport", Value: "001-Ab/09", Country: "gh"}}}}
	result = request("POST", "/v1/search", token, in, 200)
	json.Unmarshal(result, &found)
	if len(found.Matches) != 1 {
		t.Fatal("typed ID lookup failed")
	}
	in.Queries[0].Identifiers[0].Value = "1-Ab/09"
	result = request("POST", "/v1/search", token, in, 200)
	json.Unmarshal(result, &found)
	if len(found.Matches) != 0 {
		t.Fatal("leading zeros coerced")
	}
	in.Datasets = []sourcedata.DatasetRef{{Scope: "global", ID: "missing"}}
	request("POST", "/v1/search", token, in, 404)
	request("POST", "/v1/search", token, map[string]any{"tenant_id": b.String()}, 400)
	request("GET", "/v1/manual-screenings", token, nil, 404)
	oldScope, e := creds.Authenticate(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	if e = creds.Grant(ctx, client, sourcedata.DatasetRef{Scope: "tenant", ID: own.Dataset}, false); e != nil {
		t.Fatal(e)
	}
	request("POST", "/v1/enrich", token, sourcedata.CandidateRef{Dataset: sourcedata.DatasetRef{Scope: "tenant", ID: own.Dataset}, Generation: ownGen, RecordID: record.ID}, 404)
	if _, e = (internalprovider.Provider{DB: db}).GetCatalog(ctx, oldScope.Scope, sourcedata.Operation{Provider: sourcedata.Provider}); e == nil {
		t.Fatal("stale client authority accepted")
	}
	if e = creds.Revoke(ctx, client, key); e != nil {
		t.Fatal(e)
	}
	request("GET", "/v1/datasets", token, nil, 401)
	_, _, limited := create(uuid.New(), []string{"catalog"}, 2)
	request("POST", "/v1/search", limited, in, 403)
	request("GET", "/v1/datasets", limited, nil, 200)
	request("GET", "/v1/datasets", limited, nil, 200)
	request("GET", "/v1/datasets", limited, nil, 429)
	if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), record.Name) || strings.Contains(logs.String(), record.OriginalXML) {
		t.Fatal("credential/query/source leaked in logs")
	}
}

func TestStandaloneBootstrapRejectsExistingApplicationAndBudgetIsAtomic(t *testing.T) {
	db := productDB(t)
	ctx := context.Background()
	if _, e := db.Exec(ctx, `CREATE TABLE public.existing_application(id integer)`); e != nil {
		t.Fatal(e)
	}
	if e := providerproduct.Bootstrap(ctx, db); e == nil {
		t.Fatal("occupied DB adopted")
	}
	if _, e := db.Exec(ctx, `DROP TABLE public.existing_application`); e != nil {
		t.Fatal(e)
	}
	if e := providerproduct.Bootstrap(ctx, db); e != nil {
		t.Fatal(e)
	}
	creds := providerproduct.Credentials{DB: db}
	id, e := creds.Create(ctx, providerproduct.ClientInput{Name: "Budget fixture", Namespace: uuid.New(), Operations: []string{"catalog"}, RequestsPerMinute: 7, ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	var token string
	if _, e = creds.Issue(ctx, id, time.Hour, func(v string) error { token = v; return nil }); e != nil {
		t.Fatal(e)
	}
	access, e := creds.Authenticate(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 30)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- creds.Consume(ctx, access, "catalog") }()
	}
	wg.Wait()
	close(results)
	ok, limited := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if e == providerproduct.ErrQuota {
			limited++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 7 || limited != 23 {
		t.Fatal("non-atomic shared quota", ok, limited)
	}
	if _, e = db.Exec(ctx, `UPDATE provider_api.credentials SET expires_at=now()-interval '1 second'`); e != nil {
		t.Fatal(e)
	}
	if _, e = creds.Authenticate(ctx, token); e != providerproduct.ErrCredential {
		t.Fatal("expired key accepted", e)
	}
	t.Log(fmt.Sprintf("standalone bootstrap has no fraud runtime; %d concurrent requests obey a %d-request shared budget", ok+limited, ok))
}
