package postgres_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	legacy "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerregistry"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var _ ports.ScopedScreeningProvider = internalprovider.Provider{}

type internalFixture struct {
	db              *pgxpool.Pool
	a, b            provideraccess.Scope
	input           []byte
	private, global sourceingest.Key
	version         uuid.UUID
}

func newInternalFixture(t *testing.T, deterministicTenant ...uuid.UUID) internalFixture {
	t.Helper()
	db := deliveryDatabase(t)
	ctx := context.Background()
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "case-manager-service"} {
		paths, err := filepath.Glob(filepath.Join("../../../..", owner, "internal/migrations/metadata/*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal(owner, err)
		}
		for _, p := range paths {
			raw, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			execSQL(string(raw))
		}
	}
	auth, err := os.ReadFile("../../../../auth-service/internal/platform/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	execSQL(string(auth))
	principalNumber := 0
	principal := func() provideraccess.Scope {
		tenant, user, sid := uuid.New(), uuid.New(), uuid.New()
		if len(deterministicTenant) > 0 {
			tenant = uuid.NewSHA1(deterministicTenant[0], []byte(fmt.Sprint(principalNumber)))
			principalNumber++
		}
		execSQL(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Controlled fixture',$2,'active',now(),now())`, tenant, "controlled_"+strings.ReplaceAll(tenant.String(), "-", ""))
		execSQL(`INSERT INTO identity.users(id,display_name) VALUES($1,'Controlled analyst')`, user)
		execSQL(`INSERT INTO identity.memberships(user_id,tenant_id,roles) VALUES($1,$2,ARRAY['case_investigator'])`, user, tenant)
		execSQL(`INSERT INTO identity.sessions(id,user_id,tenant_id,digest,expires_at,idle_until) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 hour')`, sid, user, tenant, uuid.NewString())
		s, err := provideraccess.FromPrincipal(identity.Principal{Subject: user.String(), TenantID: tenant, SessionID: sid, AuthzVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := principal(), principal()
	input, err := os.ReadFile("../../../testdata/controlled-watchlist.json")
	if err != nil {
		t.Fatal(err)
	}
	f := internalFixture{db: db, a: a, b: b, input: input, private: sourceingest.Key{Owner: a.Tenant(), Dataset: "private-controlled"}, global: sourceingest.Key{Dataset: "global-controlled"}}
	ingest := sourceingest.Store{DB: db}
	for _, key := range []sourceingest.Key{f.private, f.global} {
		grants := []uuid.UUID{a.Tenant()}
		if key.Owner == uuid.Nil {
			grants = append(grants, b.Tenant())
		}
		if err = ingest.Register(ctx, key, "Controlled "+key.Dataset, grants); err != nil {
			t.Fatal(err)
		}
		gen, e := ingest.Stage(ctx, key, bytes.NewReader(input))
		if e != nil {
			t.Fatal(e)
		}
		published, e := ingest.Publish(ctx, key, gen)
		if e != nil {
			t.Fatal(e)
		}
		if key == f.private {
			f.version = published.Generation
		}
	}
	return f
}
func internalRequest(name, kind string, refs ...sourcedata.DatasetRef) screening.SearchRequest {
	selection, _ := json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.Profile, Threshold: 100, Datasets: refs})
	return screening.SearchRequest{Provider: sourcedata.Provider, ObjectType: kind, ObjectID: uuid.NewString(), Queries: []screening.SearchQuery{{Name: name, Type: kind}}, ProviderConfig: selection}
}

func TestInternalProviderFiveOperationsAndIsolation(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	p := internalprovider.Provider{DB: f.db}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}
	catalog, err := p.GetCatalog(ctx, f.a, sourcedata.Operation{Provider: sourcedata.Provider, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Datasets []internalprovider.Dataset `json:"datasets"`
		Next     string                     `json:"next_cursor"`
	}
	if json.Unmarshal(catalog.RawPayload, &page) != nil || len(page.Datasets) != 1 || page.Next == "" {
		t.Fatal("catalog did not page")
	}
	if _, err = p.GetCatalog(ctx, f.b, sourcedata.Operation{Provider: sourcedata.Provider, Limit: 1, Cursor: page.Next}); !errors.Is(err, sourcedata.ErrCursor) {
		t.Fatal("foreign catalog cursor", err)
	}
	if _, err = p.GetCatalog(ctx, f.a, sourcedata.Operation{Provider: sourcedata.Provider, Limit: 1, Cursor: page.Next}); err != nil {
		t.Fatal("catalog continuation", err)
	}
	missingScope := provideraccess.Scope{}
	for name, call := range map[string]func() error{
		"enrich": func() error {
			_, e := p.Enrich(ctx, missingScope, sourcedata.CandidateRef{Dataset: ref, Generation: f.version, RecordID: "controlled-person-001"})
			return e
		},
		"catalog": func() error {
			_, e := p.GetCatalog(ctx, missingScope, sourcedata.Operation{Provider: sourcedata.Provider})
			return e
		},
		"freshness": func() error {
			_, e := p.GetFreshness(ctx, missingScope, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{ref}})
			return e
		},
		"delta": func() error {
			_, e := p.GetDatasetDelta(ctx, missingScope, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{ref}})
			return e
		},
	} {
		if e := call(); !errors.Is(e, provideraccess.ErrScope) {
			t.Fatal(name, "accepted missing scope", e)
		}
	}
	for _, query := range []struct{ name, kind string }{{"Ama Testperson", "Person"}, {" AMA FIXTURE ALIAS ", "Person"}, {"Fixture Payments", "Organization"}} {
		result, err := p.Search(ctx, f.a, internalRequest(query.name, query.kind, ref))
		if err != nil || len(result.Matches) != 1 || result.Partial {
			t.Fatalf("published record not found: %+v %v", result, err)
		}
		var payload struct {
			Provenance sourcedata.CandidateRef `json:"provenance"`
		}
		if json.Unmarshal(result.Matches[0].Payload, &payload) != nil || payload.Provenance.Generation != f.version {
			t.Fatal("generation provenance missing")
		}
		if _, err = p.Enrich(ctx, f.a, payload.Provenance); err != nil {
			t.Fatal(err)
		}
		if _, err = p.Enrich(ctx, f.b, payload.Provenance); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("foreign enrichment", err)
		}
	}
	for _, scope := range []provideraccess.Scope{f.b, provideraccess.GlobalOperator()} {
		if _, err := p.Search(ctx, scope, internalRequest("Ama Testperson", "Person", ref)); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("private search leak", err)
		}
		if _, err := p.GetCatalog(ctx, scope, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{ref}}); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("private catalog leak", err)
		}
		if _, err := p.GetFreshness(ctx, scope, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{ref}}); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("private freshness leak", err)
		}
		if _, err := p.GetDatasetDelta(ctx, scope, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{ref}}); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("private delta leak", err)
		}
		catalog, err := p.GetCatalog(ctx, scope, sourcedata.Operation{Provider: sourcedata.Provider})
		if err != nil || bytes.Contains(catalog.RawPayload, []byte(f.private.Dataset)) {
			t.Fatal("private discovery leak", err)
		}
	}
	if _, err := p.Search(ctx, provideraccess.Scope{}, internalRequest("Ama Testperson", "Person", ref)); !errors.Is(err, provideraccess.ErrScope) {
		t.Fatal("missing scope accepted", err)
	}
	missing := sourcedata.DatasetRef{Scope: "tenant", ID: "missing"}
	if _, err := p.Search(ctx, f.a, internalRequest("Ama Testperson", "Person", ref, missing)); !errors.Is(err, sourcedata.ErrDataset) {
		t.Fatal("partial dataset selection ran", err)
	}
	global := sourcedata.DatasetRef{Scope: "global", ID: f.global.Dataset}
	mixed, err := p.Search(ctx, f.a, internalRequest("Ama Testperson", "Person", ref, global))
	if err != nil || len(mixed.Matches) != 2 || mixed.Matches[0].EntityID == mixed.Matches[1].EntityID {
		t.Fatal("scoped identities merged", err)
	}
	if _, err = p.GetFreshness(ctx, f.a, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{ref}}); err != nil {
		t.Fatal(err)
	}
	first, err := p.GetDatasetDelta(ctx, f.a, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{ref}})
	if err != nil || !first.Changed || first.NextCursor == "" {
		t.Fatal("meaningless delta", err)
	}
	if _, err = p.GetDatasetDelta(ctx, f.b, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{global}, Cursor: first.NextCursor}); !errors.Is(err, sourcedata.ErrCursor) {
		t.Fatal("cross-scope cursor accepted", err)
	}
	if _, err = f.db.Exec(ctx, `UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, f.a.Principal().SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = p.GetCatalog(ctx, f.a, sourcedata.Operation{Provider: sourcedata.Provider}); err == nil {
		t.Fatal("revoked scope discovered data")
	}
}

func TestControlledPublicationFailureConcurrencyAndEnrichmentHistory(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	ingest := sourceingest.Store{DB: f.db}
	bad := []byte(`{"version":1,"source":"controlled-fixture","records":[{"id":"dup","name":"One","type":"Person","source_reference":"fixture"},{"id":"dup","name":"Two","type":"Person","source_reference":"fixture"}]}`)
	if _, err := ingest.Stage(ctx, f.private, bytes.NewReader(bad)); err == nil {
		t.Fatal("duplicate accepted")
	}
	for _, raw := range []string{`{"version":1,"source":"controlled-fixture","records":[]}`, `{"version":1,"source":"other","records":[]}`, `{"version":1,"source":"controlled-fixture","records":[`} {
		if _, err := ingest.Stage(ctx, f.private, strings.NewReader(raw)); err == nil {
			t.Fatal("invalid import accepted")
		}
	}
	var active uuid.UUID
	if err := f.db.QueryRow(ctx, `SELECT active_generation FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2`, f.private.Owner, f.private.Dataset).Scan(&active); err != nil || active != f.version {
		t.Fatal("failed import changed active data", err)
	}
	unchanged, err := ingest.Stage(ctx, f.private, bytes.NewReader(f.input))
	if err != nil {
		t.Fatal(err)
	}
	same, err := ingest.Publish(ctx, f.private, unchanged)
	if err != nil || !same.Unchanged || same.Generation != f.version {
		t.Fatal("idempotency", same, err)
	}
	changed := bytes.ReplaceAll(f.input, []byte("Ama Testperson"), []byte("Changed Fixture Person"))
	g1, err := ingest.Stage(ctx, f.private, bytes.NewReader(changed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(ctx, `CREATE FUNCTION screening.reject_source_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'controlled publication failure'; END $$; CREATE TRIGGER reject_source_event BEFORE INSERT ON screening.source_publications FOR EACH ROW EXECUTE FUNCTION screening.reject_source_event()`); err != nil {
		t.Fatal(err)
	}
	if _, err = ingest.Publish(ctx, f.private, g1); err == nil {
		t.Fatal("publication failure ignored")
	}
	if err = f.db.QueryRow(ctx, `SELECT active_generation FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2`, f.private.Owner, f.private.Dataset).Scan(&active); err != nil || active != f.version {
		t.Fatal("partial publication escaped rollback", err)
	}
	if _, err = f.db.Exec(ctx, `DROP TRIGGER reject_source_event ON screening.source_publications`); err != nil {
		t.Fatal(err)
	}
	g2, err := ingest.Stage(ctx, f.private, bytes.NewReader(bytes.ReplaceAll(changed, []byte("Changed Fixture Person"), []byte("Other Fixture Person"))))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, gen := range []uuid.UUID{g1, g2} {
		wg.Add(1)
		go func(g uuid.UUID) { defer wg.Done(); _, e := ingest.Publish(ctx, f.private, g); results <- e }(gen)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, sourcedata.ErrConflict) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("concurrent publications both won", wins)
	}
	p := internalprovider.Provider{DB: f.db}
	historical := sourcedata.CandidateRef{Dataset: sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}, Generation: f.version, RecordID: "controlled-person-001"}
	enriched, err := p.Enrich(ctx, f.a, historical)
	if err != nil || !bytes.Contains(enriched.RawPayload, []byte("Ama Testperson")) {
		t.Fatal("history substituted current data", err)
	}
	delta, err := p.GetDatasetDelta(ctx, f.a, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{historical.Dataset}, Limit: 1})
	if err != nil || !delta.HasMore {
		t.Fatal("delta pagination", delta, err)
	}
	next, err := p.GetDatasetDelta(ctx, f.a, sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{historical.Dataset}, Limit: 1, Cursor: delta.NextCursor})
	if err != nil || !next.Changed || next.HasMore {
		t.Fatal("delta continuation", next, err)
	}
}

func TestControlledReaderCannotPublishOrModify(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	role := "controlled_reader_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := f.db.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, "GRANT USAGE ON SCHEMA screening,identity,core TO "+quoted+"; GRANT SELECT ON ALL TABLES IN SCHEMA identity,core TO "+quoted+"; GRANT SELECT ON screening.source_datasets,screening.source_grants,screening.source_versions,screening.source_records,screening.source_names,screening.source_publications,screening.source_read_cursors,screening.source_readiness_v1 TO "+quoted+"; GRANT INSERT ON screening.source_read_cursors TO "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg := f.db.Config()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error { _, e := conn.Exec(ctx, "SET ROLE "+quoted); return e }
	reader, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		_, _ = f.db.Exec(context.Background(), "DROP OWNED BY "+quoted)
		_, _ = f.db.Exec(context.Background(), "DROP ROLE "+quoted)
	})
	p := internalprovider.Provider{DB: reader}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}
	result, err := p.Search(ctx, f.a, internalRequest("Ama Testperson", "Person", ref))
	if err != nil || len(result.Matches) != 1 {
		t.Fatal("restricted reader cannot search", err)
	}
	if _, err = (sourceingest.Store{DB: reader}).Stage(ctx, f.private, bytes.NewReader(f.input)); !errors.Is(err, sourcedata.ErrPublish) {
		t.Fatal("reader staged publication", err)
	}
	if _, err = (sourceingest.Store{DB: reader}).Publish(ctx, f.private, f.version); !errors.Is(err, sourcedata.ErrPublish) {
		t.Fatal("reader published", err)
	}
	if _, err = reader.Exec(ctx, `UPDATE screening.source_datasets SET enabled=false`); err == nil {
		t.Fatal("reader modified datasets")
	}
}

func TestInternalProviderThroughAuthenticatedAPIWorkerAndReview(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	migrator, err := rivermigrate.New(riverpgxv5.New(f.db), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	verifier, err := identity.NewVerifier(map[string][]byte{"controlled": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})}, "controlled-test", "controlled-test")
	if err != nil {
		t.Fatal(err)
	}
	token := func(scope provideraccess.Scope) string {
		p := scope.Principal()
		claims := identity.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: p.Subject, Issuer: "controlled-test", Audience: jwt.ClaimStrings{"controlled-test"}, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, TenantID: p.TenantID.String(), SessionID: p.SessionID.String(), AuthzVersion: p.AuthzVersion, Roles: []string{"case_investigator"}}
		signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signed.Header["kid"] = "controlled"
		raw, e := signed.SignedString(key)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := httptest.NewUnstartedServer(httpapi.NewRouter(logger, f.db, httpapi.RouterConfig{InvestigatorVerifier: verifier, AuthMode: "token", AuthToken: "controlled-service-token-32-bytes", HTTPClientTimeout: time.Second}))
	if os.Getenv("CONTROLLED_BROWSER_STATE_FILE") != "" {
		listener, e := net.Listen("tcp4", "0.0.0.0:18095")
		if e != nil {
			t.Fatal(e)
		}
		api.Listener = listener
	}
	api.Start()
	if os.Getenv("CONTROLLED_BROWSER_STATE_FILE") != "" {
		api.URL = "http://127.0.0.1:18095"
	}
	defer api.Close()
	request := func(method, path, auth string, body any, want int) []byte {
		t.Helper()
		var data io.Reader
		if body != nil {
			raw, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			data = bytes.NewReader(raw)
		}
		req, e := http.NewRequest(method, api.URL+path, data)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		resp, e := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, e := io.ReadAll(resp.Body)
		if e != nil || resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s %v", method, path, resp.StatusCode, raw, e)
		}
		return raw
	}
	authA, authB := token(f.a), token(f.b)
	request("GET", "/v1/manual-screenings/capabilities", "", nil, 401)
	request("GET", "/v1/manual-screenings/capabilities", "invalid-bearer", nil, 401)
	capB := request("GET", "/v1/manual-screenings/capabilities", authB, nil, 200)
	if bytes.Contains(capB, []byte(f.private.Dataset)) {
		t.Fatal("HTTP catalog leaked private dataset")
	}
	input := service.ManualSearchInput{Provider: sourcedata.Provider, DatasetSelection: []sourcedata.DatasetRef{{Scope: "tenant", ID: f.private.Dataset}}, OperationID: uuid.NewString(), Name: "Ama Fixture Alias", EntityType: "Person", Threshold: 100, Limit: 10}
	forged, _ := json.Marshal(input)
	var forgedBody map[string]any
	json.Unmarshal(forged, &forgedBody)
	forgedBody["tenant_id"] = f.a.Tenant().String()
	request("POST", "/v1/manual-screenings", authB, forgedBody, 400)
	request("POST", "/v1/manual-screenings", authB, input, 404)
	invalid := input
	invalid.Provider = "unknown-provider"
	request("POST", "/v1/manual-screenings", authA, invalid, 400)
	created := request("POST", "/v1/manual-screenings", authA, input, 202)
	var saved struct {
		ID string `json:"id"`
	}
	json.Unmarshal(created, &saved)
	if saved.ID == "" {
		t.Fatal("screening not saved")
	}
	var jobs int
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'screening_id'=$1`, saved.ID).Scan(&jobs); err != nil || jobs < 1 {
		t.Fatal("normal queue bypassed", jobs, err)
	}
	registry := providerregistry.New(f.db, legacy.NewHTTPClient("", nil, time.Second, legacy.OpenSanctionsConfig{}))
	worker := service.NewDispatchService(store.NewTransactionManager(f.db), deliveryTestClock{}, store.NewScreeningRepository(f.db), store.NewScreeningMatchRepository(f.db), registry, nil)
	actualWorker := os.Getenv("SCREENING_WORKER_INTEGRATION_BINARY")
	if actualWorker != "" {
		startJourneyWorker(t, f.db, actualWorker, "SCREENING_PROVIDER_URL=", "SCREENING_PROVIDER_URLS=", "OPENSANCTIONS_API_HOST=", "OPENSANCTIONS_API_KEY=")
	}
	complete := func(id string) {
		t.Helper()
		if actualWorker == "" {
			if e := worker.RunScreening(ctx, f.a.Tenant().String(), id); e != nil {
				t.Fatal(e)
			}
		} else {
			waitFor(t, func() bool {
				var status string
				return f.db.QueryRow(ctx, `SELECT status FROM screening.screenings WHERE tenant_id=$1 AND id=$2`, f.a.Tenant(), id).Scan(&status) == nil && status == "awaiting_review"
			})
		}
	}
	complete(saved.ID)
	raw := request("GET", "/v1/manual-screenings/"+saved.ID, authA, nil, 200)
	var result struct {
		Status  string `json:"status"`
		Matches []struct {
			ID         string              `json:"id"`
			Properties map[string][]string `json:"properties"`
		} `json:"matches"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Status != "awaiting_review" || len(result.Matches) != 1 || result.Matches[0].Properties["datasetVersion"][0] != f.version.String() {
		t.Fatalf("existing result projection missing provenance: %s", raw)
	}
	request("GET", "/v1/manual-screenings/"+saved.ID, authB, nil, 404)
	request("POST", "/v1/manual-screenings/"+saved.ID+"/matches/"+result.Matches[0].ID+"/enrich", authB, map[string]any{}, 404)
	request("POST", "/v1/manual-screenings/"+saved.ID+"/matches/"+result.Matches[0].ID+"/enrich", authA, map[string]any{}, 200)
	history := request("GET", "/v1/manual-screenings", authA, nil, 200)
	if !bytes.Contains(history, []byte(saved.ID)) {
		t.Fatal("saved history omitted internal search")
	}
	// Produce browser-contract input from actual persisted API results, never a
	// replacement matcher. Optional artifact location is an ignored test directory.
	if output := os.Getenv("CONTROLLED_UI_EVIDENCE_FILE"); output != "" {
		if err = os.WriteFile(output, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// The real existing review transaction consumes this newly persisted candidate.
	p := f.a.Principal()
	inbox, caseID := uuid.New(), uuid.New()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Controlled review',now(),now())`, []any{inbox, p.TenantID}},
		{`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, []any{uuid.New(), p.TenantID, inbox, p.Subject}},
		{`INSERT INTO case_manager.cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES($1,$2,$3,'Controlled review','investigating','unset','manual_screening',now(),now())`, []any{caseID, p.TenantID, inbox}},
		{`INSERT INTO case_manager.case_screenings(id,tenant_id,case_id,screening_id,match_id,status,partial,created_at) VALUES($1,$2,$3,$4,$5,'pending',false,now())`, []any{uuid.New(), p.TenantID, caseID, saved.ID, result.Matches[0].ID}},
	} {
		if _, err = f.db.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	caseEnrichPath := "/v1/investigator/tenants/" + p.TenantID.String() + "/cases/" + caseID.String() + "/matches/" + result.Matches[0].ID + "/enrich"
	request("POST", caseEnrichPath, "", map[string]any{}, 401)
	request("POST", caseEnrichPath, authB, map[string]any{}, 403)
	request("POST", caseEnrichPath, authA, map[string]any{}, 200)
	svc := intakeService(f.db, intakeEnqueuer{})
	review := func(cmd *identity.Command) (identity.State, error) {
		return identity.Run(ctx, f.db, p, p.TenantID, caseID, result.Matches[0].ID, "screening", cmd, func(ctx context.Context, tx pgx.Tx, command *identity.Command) (identity.State, error) {
			return svc.InvestigatorReview(ctx, store.RepositoriesInTransaction(tx), p.TenantID.String(), result.Matches[0].ID, p.Subject, command)
		})
	}
	state, err := review(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = review(&identity.Command{OperationID: uuid.New(), ResourceID: result.Matches[0].ID, ExpectedVersion: state.Version, Action: "no_hit", Findings: "Controlled fixture reviewed"}); err != nil {
		t.Fatal(err)
	}
	org := input
	org.OperationID = uuid.NewString()
	org.EntityType = "Organization"
	org.Name = "Fixture Payments"
	created = request("POST", "/v1/manual-screenings", authA, org, 202)
	json.Unmarshal(created, &saved)
	complete(saved.ID)
	orgResult := request("GET", "/v1/manual-screenings/"+saved.ID, authA, nil, 200)
	if !bytes.Contains(orgResult, []byte("Example Fixture Payments Ltd")) {
		t.Fatal("organization missing from UI DTO")
	}
	if file := os.Getenv("CONTROLLED_BROWSER_STATE_FILE"); file != "" {
		if actualWorker == "" {
			t.Fatal("browser test requires actual worker binary")
		}
		state, _ := json.Marshal(map[string]any{"backend": "http://127.0.0.1:18095", "token": authA, "tenant": p.TenantID.String(), "actor": p.Subject, "dataset": f.private.Dataset, "generation": f.version.String(), "case_id": caseID.String(), "match_id": result.Matches[0].ID})
		if err = os.WriteFile(file, state, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Minute)
		for {
			if _, e := os.Stat(file + ".done"); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("browser handshake timed out")
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func TestControlledSourceMigrationRollback(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	for _, suffix := range []string{"000023_provider_clients.down.sql", "000022_source_statistics.down.sql", "000021_normalizer_revision.down.sql", "000020_continuous_sources.down.sql", "000019_reviewed_notices.down.sql", "000018_fuzzy_read_latency.down.sql", "000017_dataset_maintenance.down.sql", "000016_inline_evidence.down.sql", "000015_private_lists.down.sql", "000014_source_connectors.down.sql", "000013_source_lifecycle.down.sql", "000012_fuzzy_names.down.sql", "000011_controlled_sources.down.sql", "000011_controlled_sources.up.sql", "000012_fuzzy_names.up.sql", "000013_source_lifecycle.up.sql", "000014_source_connectors.up.sql", "000015_private_lists.up.sql", "000016_inline_evidence.up.sql", "000017_dataset_maintenance.up.sql", "000018_fuzzy_read_latency.up.sql", "000019_reviewed_notices.up.sql", "000020_continuous_sources.up.sql", "000021_normalizer_revision.up.sql", "000022_source_statistics.up.sql", "000023_provider_clients.up.sql"} {
		raw, err := os.ReadFile("../../migrations/metadata/" + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatal(suffix, err)
		}
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.source_datasets`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rollback/reapply failed", err)
	}
}
