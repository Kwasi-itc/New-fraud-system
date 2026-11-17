package postgres_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

type acceptanceHarness struct {
	modelURL, receiverURL, keysFile                   string
	f                                                 internalFixture
	decisionURL, screeningURL, token, human, scenario string
	client                                            *http.Client
	records                                           []sourcedata.Record
	mu                                                sync.Mutex
	workflowReceipts, eventReceipts                   map[string]bool
	stopWorkers                                       []func()
}

func acceptanceSnapshot(records []sourcedata.Record) ([]byte, error) {
	// Streaming v2 intake requires version before records to choose safe limits.
	return json.Marshal(struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Parser  string              `json:"parser"`
		Records []sourcedata.Record `json:"records"`
	}{2, "tenant-upload", "acceptance-fixture-v1", records})
}

func acceptanceDSN(db *pgxpool.Pool, max int) string {
	cfg := db.Config().ConnConfig
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), Path: "/" + cfg.Database, User: url.UserPassword(cfg.User, cfg.Password)}
	q := url.Values{"pool_max_conns": {strconv.Itoa(max)}}
	if cfg.TLSConfig == nil {
		q.Set("sslmode", "disable")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func newAcceptanceHarness(t *testing.T, count int, configure ...func(internalFixture) []string) *acceptanceHarness {
	t.Helper()
	for _, name := range []string{"DECISION_INTEGRATION_BINARY", "DECISION_WORKER_INTEGRATION_BINARY", "SCREENING_INTEGRATION_BINARY", "SCREENING_WORKER_INTEGRATION_BINARY", "CASE_MANAGER_INTEGRATION_BINARY"} {
		if os.Getenv(name) == "" {
			t.Skip("acceptance requires freshly built " + name)
		}
	}
	h := &acceptanceHarness{f: newInternalFixture(t), client: &http.Client{Timeout: 10 * time.Second}, workflowReceipts: map[string]bool{}, eventReceipts: map[string]bool{}}
	ctx := context.Background()
	db := h.f.db
	providerEnvironment := []string{}
	for _, prepare := range configure {
		providerEnvironment = append(providerEnvironment, prepare(h.f)...)
	}
	withProvider := func(base ...string) []string { return append(base, providerEnvironment...) }
	tenant := h.f.a.Tenant().String()
	if os.Getenv("SCREENING_ACCEPTANCE_SQL_PROFILE") == "true" {
		if _, e := db.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`); e != nil {
			t.Fatal(e)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	// The corpus is fictional. Four non-common name tokens keep the ordinary
	// workload measurable; high-candidate and failure cases are separate probes.
	for i := 0; i < count; i++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("acceptance-catalog-v1:%d", i)))
		words := []string{}
		for j := 0; j < 4; j++ {
			var word strings.Builder
			for k := 0; k < 6; k++ {
				word.WriteByte('a' + sum[j*6+k]%26)
			}
			words = append(words, word.String())
		}
		kind := "Person"
		if i%4 == 3 {
			kind = "Organization"
		}
		h.records = append(h.records, sourcedata.Record{ID: fmt.Sprintf("accept-%06d", i), Name: strings.Join(words, " "), Type: kind, SourceReference: "Fictional acceptance workload, not an official designation"})
	}
	if count > 0 {
		raw, marshalErr := acceptanceSnapshot(h.records)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		ingest := sourceingest.Store{DB: db}
		gen, e := ingest.Stage(ctx, h.f.private, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ingest.Publish(ctx, h.f.private, gen); e != nil {
			t.Fatal(e)
		}
		if os.Getenv("SCREENING_ACCEPTANCE_SQL_PROFILE") == "true" {
			plan := func(label string) {
				t.Helper()
				sql, args, e := internalprovider.CandidateStatement([]internalprovider.CandidateSelection{{Owner: h.f.private.Owner, Dataset: h.f.private.Dataset, Generation: gen}}, []screening.SearchQuery{{Name: h.records[0].Name, Type: "Person"}})
				if e != nil {
					t.Fatal(e)
				}
				tx, e := db.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(ctx)
				if _, e = tx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.15',true)`); e != nil {
					t.Fatal(e)
				}
				var raw json.RawMessage
				if e = tx.QueryRow(ctx, "EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) "+sql, args...).Scan(&raw); e != nil {
					t.Fatal(e)
				}
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				acceptanceReport(t, label, raw)
			}
			plan("candidate-plan-fresh")
			if os.Getenv("SCREENING_ACCEPTANCE_ANALYZE") == "true" {
				if _, e = db.Exec(ctx, `ANALYZE screening.source_names;ANALYZE screening.source_records;ANALYZE screening.source_identifiers`); e != nil {
					t.Fatal(e)
				}
				plan("candidate-plan-analyzed")
			}
		}
	}
	h.scenario = uuid.NewString()
	iteration, inbox, config := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Acceptance payment checks','transaction',now(),now())`, h.scenario, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,trigger_formula,score_review_threshold,score_block_and_review_threshold,score_decline_threshold,created_at) VALUES($1,$2,$3,1,'live','{"constant":true}',10,30,90,now())`, iteration, h.scenario, tenant)
	exec(`UPDATE core.scenarios SET live_iteration_id=$1 WHERE id=$2`, iteration, h.scenario)
	for i, r := range []struct {
		name, formula string
		score         int
	}{{"High amount", `{"function":"gt","children":[{"function":"Payload","children":[{"constant":"amount"}]},{"constant":10000}]}`, 20}, {"Risk country", `{"function":"eq","children":[{"function":"Payload","children":[{"constant":"country"}]},{"constant":"IR"}]}`, 15}} {
		exec(`INSERT INTO core.rules(id,iteration_id,tenant_id,display_order,name,formula,score_modifier,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,now(),now())`, uuid.NewString(), iteration, tenant, i, r.name, r.formula, r.score)
	}
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Acceptance review',now(),now())`, inbox, tenant)
	selection := sourcedata.Selection{Version: 2, Profile: sourcedata.FuzzyProfile, Threshold: 85, Datasets: []sourcedata.DatasetRef{{Scope: "tenant", ID: h.f.private.Dataset}}}
	cfg, _ := json.Marshal(map[string]any{"authoring_version": 2, "entity_type": "Person", "query_fields": map[string]string{"name": "beneficiary_name"}, "provider_config": selection, "forced_outcome": "block_and_review"})
	exec(`INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,allowed_outcomes,provider,config_json,active,review_inbox_id,created_at,updated_at) VALUES($1,$2,$3,'Acceptance inline screening','{approve,review,block_and_review,decline}','internal-watchlists',$4,true,$5,now(),now())`, config, tenant, h.scenario, cfg, inbox)
	migrator, e := rivermigrate.New(riverpgxv5.New(db), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); e != nil {
		t.Fatal(e)
	}
	// The model read is a controlled HTTP dependency, explicitly outside the
	// benchmark claim; evaluation, SQL, matching, persistence and workers are real.
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tenants/"+tenant+"/data-model" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data_model": map[string]any{"revision_id": "acceptance-v1", "tables": map[string]any{"transaction": map[string]any{"name": "transaction", "fields": map[string]any{"beneficiary_name": map[string]any{"name": "beneficiary_name", "data_type": "string", "nullable": false}, "amount": map[string]any{"name": "amount", "data_type": "float", "nullable": false}, "country": map[string]any{"name": "country", "data_type": "string", "nullable": false}}, "links_to_single": map[string]any{}}}}})
	}))
	t.Cleanup(model.Close)
	h.modelURL = model.URL
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/workflow":
			if r.Header.Get("Authorization") != "" {
				t.Error("service credential leaked to workflow URL")
				w.WriteHeader(403)
				return
			}
			id, _ := body["workflow_execution_id"].(string)
			if id == "" {
				w.WriteHeader(400)
				return
			}
			h.workflowReceipts[id] = true
		case "/events":
			if r.Header.Get("Authorization") != "Bearer continuous-intake-integration-test-token-32-bytes" {
				w.WriteHeader(401)
				return
			}
			id, _ := body["event_id"].(string)
			if id == "" {
				w.WriteHeader(400)
				return
			}
			h.eventReceipts[id] = true
		default:
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(receiver.Close)
	h.receiverURL = receiver.URL
	workflowCfg, _ := json.Marshal(map[string]string{"url": receiver.URL + "/workflow"})
	exec(`INSERT INTO core.workflows(id,tenant_id,scenario_id,name,allowed_outcomes,action_type,action_config,active,created_at,updated_at) VALUES($1,$2,$3,'Acceptance receipt','{approve,review,block_and_review,decline}','emit_event',$4,true,now(),now())`, uuid.NewString(), tenant, h.scenario, workflowCfg)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	keys, _ := json.Marshal(map[string]string{"acceptance": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))})
	keysFile := filepath.Join(t.TempDir(), "keys.json")
	h.keysFile = keysFile
	if e = os.WriteFile(keysFile, keys, 0600); e != nil {
		t.Fatal(e)
	}
	p := h.f.a.Principal()
	claims := jwt.MapClaims{"sub": p.Subject, "tenant_id": tenant, "sid": p.SessionID.String(), "authz_version": p.AuthzVersion, "roles": []string{"case_investigator"}, "iss": "intake-test", "aud": "intake-test", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
	signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed.Header["kid"] = "acceptance"
	h.human, e = signed.SignedString(key)
	if e != nil {
		t.Fatal(e)
	}
	h.screeningURL, h.token, _ = startInvestigatorServer(t, db, os.Getenv("SCREENING_INTEGRATION_BINARY"), tenant, withProvider("DATABASE_URL="+acceptanceDSN(db, 4), "USER_JWT_KEYS_FILE="+keysFile, "USER_REQUIRE_SESSION=true", "SCREENING_PROVIDER_URL=", "OPENSANCTIONS_API_HOST=", "OPENSANCTIONS_API_KEY=")...)
	h.decisionURL, _, _ = startInvestigatorServer(t, db, os.Getenv("DECISION_INTEGRATION_BINARY"), tenant, withProvider("DATABASE_URL="+acceptanceDSN(db, 16), "DATA_MODEL_SERVICE_URL="+model.URL, "INTERNAL_SCREENING_INLINE_ENABLED=true", "INTERNAL_SCREENING_FAILURE_POLICY=error", "INTERNAL_SCREENING_TIMEOUT=2s", "RULE_EVALUATION_CONCURRENCY=2")...)
	caseURL, _ := startCaseServer(t, db, os.Getenv("CASE_MANAGER_INTEGRATION_BINARY"), tenant)
	h.stopWorkers = append(h.stopWorkers, startJourneyWorker(t, db, os.Getenv("SCREENING_WORKER_INTEGRATION_BINARY"), withProvider("DATABASE_URL="+acceptanceDSN(db, 4), "GOMAXPROCS=2", "SCREENING_PROVIDER_URL=", "OPENSANCTIONS_API_HOST=", "OPENSANCTIONS_API_KEY=", "CASE_SERVICE_URL="+caseURL, "CASE_SERVICE_AUTH_TOKEN="+h.token, "DECISION_ENGINE_URL="+h.decisionURL)...))
	h.stopWorkers = append(h.stopWorkers, startJourneyWorker(t, db, os.Getenv("DECISION_WORKER_INTEGRATION_BINARY"), withProvider("DATABASE_URL="+acceptanceDSN(db, 4), "GOMAXPROCS=2", "OUTBOX_PUBLISHER_URL="+receiver.URL+"/events", "WORKFLOW_DISPATCH_QUEUE_WORKERS=4", "OUTBOX_QUEUE_WORKERS=8", "RIVER_FETCH_COOLDOWN=5ms", "RIVER_FETCH_POLL_INTERVAL=100ms", "SCREENING_SERVICE_URL="+h.screeningURL)...))
	return h
}

func (h *acceptanceHarness) request(method, path, token string, body any) (int, []byte, error) {
	raw, e := json.Marshal(body)
	if e != nil {
		return 0, nil, e
	}
	r, e := http.NewRequest(method, path, bytes.NewReader(raw))
	if e != nil {
		return 0, nil, e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	res, e := h.client.Do(r)
	if e != nil {
		return 0, nil, e
	}
	defer res.Body.Close()
	data, e := io.ReadAll(res.Body)
	return res.StatusCode, data, e
}

func acceptanceReport(t *testing.T, name string, value any) {
	t.Helper()
	raw, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("SCREENING_ACCEPTANCE_REPORT_DIR"); dir != "" {
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name+".json"), append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Log("ACCEPTANCE_REPORT " + name + " " + string(raw))
}
