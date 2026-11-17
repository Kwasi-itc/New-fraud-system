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
	legacyclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerregistry"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func TestInternalProviderScenarioAuthoringThroughRealWorkers(t *testing.T) {
	binaries := map[string]string{}
	for _, key := range []string{"DECISION_INTEGRATION_BINARY", "SCREENING_INTEGRATION_BINARY", "SCREENING_WORKER_INTEGRATION_BINARY", "DECISION_WORKER_INTEGRATION_BINARY", "CASE_MANAGER_INTEGRATION_BINARY"} {
		binaries[key] = os.Getenv(key)
		if binaries[key] == "" {
			t.Skip("fresh binary required: " + key)
		}
	}
	f := newInternalFixture(t)
	db := f.db
	ctx := context.Background()
	principal := f.a.Principal()
	tenant := f.a.Tenant().String()
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	v2, err := os.ReadFile("../../../../auth-service/internal/platform/schema_v2.sql")
	if err != nil {
		t.Fatal(err)
	}
	execSQL(string(v2))
	execSQL(`UPDATE identity.memberships SET roles=ARRAY['platform_admin','case_investigator'] WHERE tenant_id=$1 AND user_id=$2`, tenant, principal.Subject)
	scenario, iteration, table, inbox := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	execSQL(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Controlled internal scenario','transaction',now(),now())`, scenario, tenant)
	execSQL(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,trigger_formula,created_at) VALUES($1,$2,$3,1,'live','{"constant":true}',now())`, iteration, scenario, tenant)
	execSQL(`UPDATE core.scenarios SET live_iteration_id=$1 WHERE id=$2`, iteration, scenario)
	execSQL(`INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'transaction',now(),now())`, table, tenant)
	execSQL(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,'beneficiary_name','string',now(),now())`, uuid.NewString(), tenant, table)
	execSQL(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Controlled review',now(),now())`, inbox, tenant)
	execSQL(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, inbox, principal.Subject)
	migrator, err := rivermigrate.New(riverpgxv5.New(db), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	var legacyCalls atomic.Int64
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyCalls.Add(1)
		http.Error(w, "unexpected legacy provider", 503)
	}))
	defer legacy.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tenants/"+tenant+"/data-model" {
			http.Error(w, "not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data_model": map[string]any{"revision_id": "controlled-phase2", "tables": map[string]any{"transaction": map[string]any{"name": "transaction", "fields": map[string]any{"beneficiary_name": map[string]any{"name": "beneficiary_name", "data_type": "string", "nullable": false}}, "links_to_single": map[string]any{}}}}})
	}))
	defer model.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	keyJSON, _ := json.Marshal(map[string]string{"phase2": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))})
	keyFile := filepath.Join(t.TempDir(), "keys.json")
	if err = os.WriteFile(keyFile, keyJSON, 0600); err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{"sub": principal.Subject, "tenant_id": tenant, "sid": principal.SessionID.String(), "authz_version": 1, "roles": []string{"case_investigator"}, "iss": "intake-test", "aud": "intake-test", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
	signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed.Header["kid"] = "phase2"
	humanToken, err := signed.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	sourceURL, token, _ := startInvestigatorServer(t, db, binaries["SCREENING_INTEGRATION_BINARY"], tenant, "SCREENING_PROVIDER_URL="+legacy.URL, "SCREENING_PROVIDER_URLS=", "OPENSANCTIONS_API_HOST=", "OPENSANCTIONS_API_KEY=", "USER_JWT_KEYS_FILE="+keyFile, "USER_REQUIRE_SESSION=true")
	const authoring = "controlled-phase2-authoring-private-token-32"
	decisionURL, _, _ := startInvestigatorServer(t, db, binaries["DECISION_INTEGRATION_BINARY"], tenant, "SCREENING_AUTHORING_TOKEN="+authoring, "DATA_MODEL_SERVICE_URL="+model.URL, "DATA_MODEL_SERVICE_TOKEN="+token, "SCREENING_SERVICE_URL="+sourceURL)
	caseURL, _ := startCaseServer(t, db, binaries["CASE_MANAGER_INTEGRATION_BINARY"], tenant)
	client := &http.Client{Timeout: 10 * time.Second}
	request := func(base, method, path, bearer string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r, e := http.NewRequest(method, base+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Screening-User", principal.Subject)
		r.Header.Set("X-Screening-Session", principal.SessionID.String())
		r.Header.Set("X-Screening-Version", "1")
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d want %d %s", method, path, resp.StatusCode, want, data)
		}
		out := map[string]any{}
		json.Unmarshal(data, &out)
		return out
	}
	base := "/internal/screening-authoring/tenants/" + tenant + "/scenarios/" + scenario
	configs := []string{}
	for _, entity := range []string{"Person", "Organization"} {
		id := uuid.NewString()
		configs = append(configs, id)
		rule := map[string]any{"id": id, "provider": "internal-watchlists", "profile": "name-fuzzy-v1", "dataset_selection": []map[string]string{{"scope": "tenant", "id": f.private.Dataset}}, "threshold": 85, "result_limit": 10, "name": entity + " controlled screening", "enabled": true, "entity_type": entity, "name_field": "beneficiary_name", "outcomes": []string{"approve"}, "inbox_id": inbox}
		saved := request(decisionURL, "PUT", base+"/configs/"+id, authoring, rule, 200)
		if saved["provider"] != "internal-watchlists" || saved["profile"] != "name-fuzzy-v1" {
			t.Fatal("selection lost on save", saved)
		}
		bad := map[string]any{}
		for k, v := range rule {
			bad[k] = v
		}
		bad["provider"] = "unknown"
		bad["updated_at"] = saved["updated_at"]
		request(decisionURL, "PUT", base+"/configs/"+id, authoring, bad, 400)
	}
	page := request(decisionURL, "GET", base, authoring, nil, 200)
	if len(page["rules"].([]any)) != 2 || len(page["providers"].([]any)) != 2 {
		t.Fatal("saved discovery/reopen", page)
	}
	request(decisionURL, "GET", "/internal/screening-authoring/tenants/"+f.b.Tenant().String()+"/scenarios/"+scenario, authoring, nil, 403)
	startJourneyWorker(t, db, binaries["SCREENING_WORKER_INTEGRATION_BINARY"], "SCREENING_PROVIDER_URL="+legacy.URL, "CASE_SERVICE_URL="+caseURL, "CASE_SERVICE_AUTH_TOKEN="+token, "DECISION_ENGINE_URL="+decisionURL)
	startJourneyWorker(t, db, binaries["DECISION_WORKER_INTEGRATION_BINARY"], "SCREENING_SERVICE_URL="+sourceURL)
	for _, name := range []string{"Ama Testpreson", "Fixture Paymnts"} {
		out := request(decisionURL, "POST", "/v1/tenants/"+tenant+"/scenarios/"+scenario+"/evaluate", token, map[string]any{"object_id": uuid.NewString(), "object_type": "transaction", "fields": map[string]any{"beneficiary_name": name}}, 200)
		decision := out["result"].(map[string]any)["decision"].(map[string]any)["id"].(string)
		waitFor(t, func() bool {
			var n int
			return db.QueryRow(ctx, `SELECT count(*) FROM core.screening_executions WHERE tenant_id=$1 AND decision_id=$2 AND status='completed'`, tenant, decision).Scan(&n) == nil && n == 2
		})
		var matches, authority int
		if err = db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_matches m JOIN screening.screenings s ON (s.id,s.tenant_id)=(m.screening_id,m.tenant_id) WHERE s.tenant_id=$1 AND s.decision_id=$2 AND s.provider='internal-watchlists' AND m.payload->>'profile'='name-fuzzy-v1'`, tenant, decision).Scan(&matches); err != nil || matches != 1 {
			t.Fatal("actual internal result", matches, err)
		}
		if err = db.QueryRow(ctx, `SELECT count(*) FROM screening.provider_run_authority a JOIN screening.screenings s ON (s.id,s.tenant_id)=(a.screening_id,a.tenant_id) WHERE s.tenant_id=$1 AND s.decision_id=$2 AND a.authority->>'kind'='scenario-execution'`, tenant, decision).Scan(&authority); err != nil || authority != 2 {
			t.Fatal("durable authority missing", authority, err)
		}
		waitFor(t, func() bool {
			var n int
			return db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_decisions WHERE tenant_id=$1 AND decision_id=$2`, tenant, decision).Scan(&n) == nil && n == 1
		})
		var caseID, matchID string
		waitFor(t, func() bool {
			return db.QueryRow(ctx, `SELECT cs.case_id::text,m.id FROM case_manager.case_screenings cs JOIN screening.screening_matches m ON (m.tenant_id,m.screening_id,m.id)=(cs.tenant_id,cs.screening_id,cs.match_id) JOIN screening.screenings s ON (s.tenant_id,s.id)=(m.tenant_id,m.screening_id) WHERE s.tenant_id=$1 AND s.decision_id=$2 AND m.score>=0.85`, tenant, decision).Scan(&caseID, &matchID) == nil
		})
		enrichPath := "/v1/investigator/tenants/" + tenant + "/cases/" + caseID + "/matches/" + matchID + "/enrich"
		request(sourceURL, "POST", enrichPath, humanToken, map[string]any{}, 200)
		reviewPath := "/v1/investigator/tenants/" + tenant + "/cases/" + caseID + "/review"
		state := request(sourceURL, "GET", reviewPath+"?resource_id="+matchID, humanToken, nil, 200)
		request(sourceURL, "POST", reviewPath, humanToken, map[string]any{"operation_id": uuid.NewString(), "resource_id": matchID, "expected_version": state["version"], "action": "no_hit", "findings": "Reviewed controlled internal scenario candidate"}, 200)
		var sourceID string
		if err = db.QueryRow(ctx, `SELECT screening_id::text FROM screening.screening_matches WHERE tenant_id=$1 AND id=$2`, tenant, matchID).Scan(&sourceID); err != nil {
			t.Fatal(err)
		}
		item, err := store.NewScreeningRepository(db).GetByID(ctx, tenant, sourceID)
		if err != nil {
			t.Fatal(err)
		}
		registry := providerregistry.New(db, legacyclient.NewHTTPClient(legacy.URL, nil, time.Second, legacyclient.OpenSanctionsConfig{}))
		jobContext, err := registry.ForRun(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		jobScope, err := provideraccess.FromContext(jobContext)
		if err != nil {
			t.Fatal(err)
		}
		outside := sourcedata.DatasetRef{Scope: "global", ID: f.global.Dataset}
		operation := sourcedata.Operation{Provider: sourcedata.Provider, Selection: []sourcedata.DatasetRef{outside}}
		if _, err = registry.Internal.Search(ctx, jobScope, internalRequest("Ama Testperson", "Person", outside)); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("job expanded search selection", err)
		}
		if _, err = registry.Internal.Enrich(ctx, jobScope, sourcedata.CandidateRef{Dataset: outside, Generation: f.version, RecordID: "controlled-person-001"}); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("job expanded enrichment selection", err)
		}
		if _, err = registry.Internal.GetCatalog(ctx, jobScope, operation); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("job expanded catalog selection", err)
		}
		if _, err = registry.Internal.GetFreshness(ctx, jobScope, operation); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("job expanded freshness selection", err)
		}
		if _, err = registry.Internal.GetDatasetDelta(ctx, jobScope, operation); !errors.Is(err, sourcedata.ErrDataset) {
			t.Fatal("job expanded delta selection", err)
		}
		var original []byte
		if err = db.QueryRow(ctx, `SELECT request_json FROM core.screening_executions WHERE tenant_id=$1 AND decision_id=$2 AND config_id=$3`, tenant, decision, configs[0]).Scan(&original); err != nil {
			t.Fatal(err)
		}
		var forged map[string]any
		json.Unmarshal(original, &forged)
		forged["queries"] = []map[string]string{{"name": "Forged Name", "type": "Person"}}
		request(sourceURL, "POST", "/internal/v1/tenants/"+tenant+"/decision-screenings", testIntakeToken, forged, 400)
		request(sourceURL, "POST", "/internal/v1/tenants/"+f.b.Tenant().String()+"/decision-screenings", testIntakeToken, json.RawMessage(original), 400)
		request(sourceURL, "POST", "/internal/v1/tenants/"+tenant+"/decision-screenings", "invalid-token", json.RawMessage(original), 401)
	}
	if legacyCalls.Load() != 0 {
		t.Fatal("internal selection called legacy/synthetic adapter", legacyCalls.Load())
	}
	// Revoked private grants fail at intake/execution, without a false no-hit.
	execSQL(`DELETE FROM screening.source_grants WHERE tenant_id=$1 AND owner_scope=$1 AND dataset_id=$2`, tenant, f.private.Dataset)
	out := request(decisionURL, "POST", "/v1/tenants/"+tenant+"/scenarios/"+scenario+"/evaluate", token, map[string]any{"object_id": uuid.NewString(), "object_type": "transaction", "fields": map[string]any{"beneficiary_name": "Ama Testperson"}}, 200)
	decision := out["result"].(map[string]any)["decision"].(map[string]any)["id"].(string)
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM core.screening_executions WHERE tenant_id=$1 AND decision_id=$2 AND status='failed'`, tenant, decision).Scan(&n) == nil && n == 2
	})
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM screening.screenings WHERE tenant_id=$1 AND decision_id=$2`, tenant, decision).Scan(&count); err != nil || count != 0 {
		t.Fatal("revocation created completed search", count, err)
	}
}
