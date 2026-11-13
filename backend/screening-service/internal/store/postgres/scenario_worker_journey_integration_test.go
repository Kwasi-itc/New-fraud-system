package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runAuthoredScreeningWorkers(t *testing.T, db *pgxpool.Pool, tenant, inbox, decisionURL, token, operatorToken string, evaluate func(string) string) {
	t.Helper()
	binaries := map[string]string{}
	for _, key := range []string{"SCREENING_INTEGRATION_BINARY", "CASE_MANAGER_INTEGRATION_BINARY", "DECISION_WORKER_INTEGRATION_BINARY", "SCREENING_WORKER_INTEGRATION_BINARY"} {
		binaries[key] = os.Getenv(key)
		if binaries[key] == "" {
			t.Skip("local worker journey requires " + key)
		}
	}
	ctx := context.Background()
	var mu sync.Mutex
	calls := map[string]int{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.Method != "POST" {
			http.Error(w, "unexpected request", 400)
			return
		}
		var req screening.SearchRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Queries) != 1 || req.Provider != "opensanctions" || req.DecisionID == "" || req.ScreeningConfigID == "" {
			http.Error(w, "invalid provider contract", 400)
			return
		}
		mu.Lock()
		calls[req.IdempotencyKey]++
		attempt := calls[req.IdempotencyKey]
		mu.Unlock()
		if attempt == 1 {
			switch req.Queries[0].Name {
			case "Synthetic Provider Failure":
				http.Error(w, "private-provider-body-do-not-persist", 503)
				return
			case "Synthetic Provider Timeout":
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
				return
			}
		}
		result := screening.ProviderResult{Matches: []screening.ProviderMatch{}}
		switch req.Queries[0].Name {
		case "Synthetic Empty":
		case "Synthetic Partial":
			result.Partial = true
		default:
			result.Matches = []screening.ProviderMatch{{EntityID: "synthetic-entity", Name: "Synthetic Example", Score: 0.95, Payload: json.RawMessage(`{"datasets":["synthetic"],"schema":"Person"}`)}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	t.Cleanup(provider.Close)
	sourceOperatorToken := "synthetic-source-recovery-operator-token-32"
	screeningURL, _, _ := startInvestigatorServer(t, db, binaries["SCREENING_INTEGRATION_BINARY"], tenant,
		"SCREENING_OPERATOR_TOKEN="+sourceOperatorToken, "SCREENING_PROVIDER_URL="+provider.URL, "SCREENING_PROVIDER_URLS=", "OPENSANCTIONS_API_HOST=", "OPENSANCTIONS_API_KEY=")
	caseURL, _ := startCaseServer(t, db, binaries["CASE_MANAGER_INTEGRATION_BINARY"], tenant)
	targetURL, _ := url.Parse(screeningURL)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.ModifyResponse = func(resp *http.Response) error {
		if execution := resp.Request.Header.Get("X-Synthetic-Late-Ack"); execution != "" && resp.StatusCode == 201 {
			// Delay the dispatch acknowledgement until the source and decision
			// projection complete, then report a synthetic lost acknowledgement.
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				var completed bool
				err := db.QueryRow(resp.Request.Context(), `SELECT status='completed' AND projection_source_id IS NOT NULL
					FROM core.screening_executions WHERE id=$1 AND tenant_id=$2`, execution, tenant).Scan(&completed)
				if err == nil && completed {
					mu.Lock()
					calls["late-ack:"+execution]++
					mu.Unlock()
					resp.Body.Close()
					resp.StatusCode = 503
					body := []byte(`{"error":"synthetic lost acknowledgement"}`)
					resp.Body = io.NopCloser(bytes.NewReader(body))
					resp.ContentLength = int64(len(body))
					resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
					break
				}
				select {
				case <-resp.Request.Context().Done():
					return resp.Request.Context().Err()
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		if resp.StatusCode >= 400 {
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(raw))
			t.Logf("synthetic intake failure: %d %s", resp.StatusCode, raw)
			return err
		}
		return nil
	}
	intake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var req screening.SearchRequest
		_ = json.Unmarshal(raw, &req)
		if len(req.Queries) == 1 && req.Queries[0].Name == "Synthetic Late Acknowledgement" {
			r.Header.Set("X-Synthetic-Late-Ack", req.IdempotencyKey)
		}
		if len(req.Queries) == 1 && req.Queries[0].Name == "Synthetic Intake Recovery" {
			mu.Lock()
			calls["intake:"+req.IdempotencyKey]++
			first := calls["intake:"+req.IdempotencyKey] == 1
			mu.Unlock()
			if first {
				http.Error(w, "synthetic private failure", 503)
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(intake.Close)
	startJourneyWorker(t, db, binaries["SCREENING_WORKER_INTEGRATION_BINARY"],
		"SCREENING_PROVIDER_URL="+provider.URL, "CASE_SERVICE_URL="+caseURL, "CASE_SERVICE_AUTH_TOKEN="+token, "DECISION_ENGINE_URL="+decisionURL, "HTTP_CLIENT_TIMEOUT=1s")
	startJourneyWorker(t, db, binaries["DECISION_WORKER_INTEGRATION_BINARY"],
		"SCREENING_SERVICE_URL="+intake.URL)
	// Existing authored executions are consumed too. Their two matching configs
	// exercise Person/Organization requests before the later disable took effect.
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM core.screening_executions WHERE status='completed'`).Scan(&n) == nil && n == 3
	})
	t.Run("explicit retry after intake failure", func(t *testing.T) {
		decision := evaluate("Synthetic Intake Recovery")
		var execution string
		waitFor(t, func() bool {
			return db.QueryRow(ctx, `SELECT e.id::text FROM core.screening_executions e JOIN river_job j ON j.args->>'execution_id'=e.id::text
				WHERE e.decision_id=$1 AND e.status='failed' AND j.kind='screening_execution' AND j.state='discarded'`, decision).Scan(&execution) == nil
		})
		var sources int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screenings WHERE decision_id=$1`, decision).Scan(&sources); err != nil || sources != 0 {
			t.Fatal("rejected intake persisted source", sources, err)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		retry := func(want int) {
			t.Helper()
			req, _ := http.NewRequest("POST", decisionURL+"/v1/tenants/"+tenant+"/screening-executions/"+execution+"/retry", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			if strings.HasPrefix(req.URL.String(), decisionURL+"/v1/") {
				req.Header.Set("X-Screening-Authoring-Token", operatorToken)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != want {
				t.Fatalf("retry status=%d want=%d body=%s", resp.StatusCode, want, raw)
			}
		}
		// A failed enqueue must leave the failed execution intact.
		if _, err := db.Exec(ctx, `CREATE TRIGGER reject_journey_queue BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION public.reject_journey_queue()`); err != nil {
			t.Fatal(err)
		}
		retry(400)
		var status string
		if err := db.QueryRow(ctx, `SELECT status FROM core.screening_executions WHERE id=$1`, execution).Scan(&status); err != nil || status != "failed" {
			t.Fatal("retry failed to roll back", status, err)
		}
		if _, err := db.Exec(ctx, `DROP TRIGGER reject_journey_queue ON river_job`); err != nil {
			t.Fatal(err)
		}
		retry(200)
		waitFor(t, func() bool {
			return db.QueryRow(ctx, `SELECT status FROM core.screening_executions WHERE id=$1`, execution).Scan(&status) == nil && status == "completed"
		})
		retry(400)
		waitFor(t, func() bool {
			var links int
			return db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_screenings cs JOIN screening.screenings s ON s.id=cs.screening_id
				WHERE s.decision_id=$1 AND s.tenant_id=$2`, decision, tenant).Scan(&links) == nil && links == 1
		})
		var jobs int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind='screening_execution' AND args->>'execution_id'=$1`, execution).Scan(&jobs); err != nil || jobs != 2 {
			t.Fatal("retry job count", jobs, err)
		}
		mu.Lock()
		attempted := calls["intake:"+execution]
		providerCalls := calls[execution]
		mu.Unlock()
		if attempted != 2 || providerCalls != 1 {
			t.Fatal("unexpected retry delivery", attempted, providerCalls)
		}
	})
	for _, name := range []string{"Synthetic Provider Failure", "Synthetic Provider Timeout"} {
		t.Run(name, func(t *testing.T) {
			decision := evaluate(name)
			var source, execution, failure string
			waitFor(t, func() bool {
				return db.QueryRow(ctx, `SELECT s.id::text,e.id::text,s.last_error FROM core.screening_executions e
					JOIN screening.screenings s ON s.tenant_id=e.tenant_id AND s.idempotency_key=e.id::text
					WHERE e.decision_id=$1 AND e.status='failed' AND s.status='failed'`, decision).Scan(&source, &execution, &failure) == nil
			})
			if failure == "" || strings.Contains(failure, provider.URL) || strings.Contains(failure, "private-provider-body") || strings.Contains(failure, name) {
				t.Fatal("unsafe or missing provider failure", failure)
			}
			var matches, events int
			if err := db.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1),
				(SELECT count(*) FROM screening.case_event_outbox WHERE payload->>'screening_id'=$1::uuid::text)`, source).Scan(&matches, &events); err != nil || matches != 0 || events != 0 {
				t.Fatal("provider failure published results", matches, events, err)
			}
			client := &http.Client{Timeout: 5 * time.Second}
			post := func(endpoint string) (int, error) {
				req, err := http.NewRequest("POST", endpoint, nil)
				if err != nil {
					return 0, err
				}
				req.Header.Set("Authorization", "Bearer "+token)
				if strings.HasPrefix(req.URL.String(), decisionURL+"/v1/") {
					req.Header.Set("X-Screening-Authoring-Token", operatorToken)
				}
				if strings.HasPrefix(endpoint, screeningURL+"/v1/") {
					req.Header.Set("X-Screening-Operator-Token", sourceOperatorToken)
				}
				resp, err := client.Do(req)
				if err != nil {
					return 0, err
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				return resp.StatusCode, nil
			}
			// The decision projection is not the owner of provider retry state.
			if code, err := post(decisionURL + "/v1/tenants/" + tenant + "/screening-executions/" + execution + "/retry"); err != nil || code != 400 {
				t.Fatal("decision reset source-managed failure", code, err)
			}
			retryURL := screeningURL + "/v1/tenants/" + tenant + "/screenings/" + source + "/retry"
			deniedRequest, _ := http.NewRequest("POST", retryURL, nil)
			deniedRequest.Header.Set("Authorization", "Bearer "+token)
			deniedResponse, err := client.Do(deniedRequest)
			if err != nil {
				t.Fatal(err)
			}
			deniedResponse.Body.Close()
			if deniedResponse.StatusCode != 403 {
				t.Fatal("general credential recovered source", deniedResponse.StatusCode)
			}
			var unchanged string
			if err := db.QueryRow(ctx, `SELECT status FROM screening.screenings WHERE id=$1`, source).Scan(&unchanged); err != nil || unchanged != "failed" {
				t.Fatal("denied retry changed source", unchanged, err)
			}
			if _, err := db.Exec(ctx, `CREATE OR REPLACE FUNCTION public.reject_provider_retry() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN IF NEW.kind='screening_dispatch' THEN RAISE EXCEPTION 'synthetic provider retry queue failure'; END IF; RETURN NEW; END $$;
				CREATE TRIGGER reject_provider_retry BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION public.reject_provider_retry()`); err != nil {
				t.Fatal(err)
			}
			if code, err := post(retryURL); err != nil || code != 400 {
				t.Fatal("expected retry enqueue failure", code, err)
			}
			var status string
			if err := db.QueryRow(ctx, `SELECT status FROM screening.screenings WHERE id=$1`, source).Scan(&status); err != nil || status != "failed" {
				t.Fatal("partial retry update", status, err)
			}
			if _, err := db.Exec(ctx, `DROP TRIGGER reject_provider_retry ON river_job`); err != nil {
				t.Fatal(err)
			}
			// Race actual authorized HTTP requests, not repository stubs.
			var wg sync.WaitGroup
			codes := make(chan int, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					code, err := post(retryURL)
					if err != nil {
						codes <- 0
					} else {
						codes <- code
					}
				}()
			}
			wg.Wait()
			close(codes)
			wins, denials := 0, 0
			for code := range codes {
				if code == 200 {
					wins++
				} else if code == 400 {
					denials++
				} else {
					t.Fatal("unexpected retry response", code)
				}
			}
			if wins != 1 || denials != 1 {
				t.Fatal("competing retries", wins, denials)
			}
			waitFor(t, func() bool {
				return db.QueryRow(ctx, `SELECT status FROM core.screening_executions WHERE id=$1`, execution).Scan(&status) == nil && status == "completed"
			})
			waitFor(t, func() bool {
				var links int
				return db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_screenings cs JOIN case_manager.cases c ON c.id=cs.case_id
					WHERE cs.screening_id=$1 AND c.tenant_id=$2 AND c.inbox_id=$3`, source, tenant, inbox).Scan(&links) == nil && links == 1
			})
			if code, err := post(retryURL); err != nil || code != 400 {
				t.Fatal("completed source retried", code, err)
			}
			var jobs int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind='screening_dispatch' AND args->>'screening_id'=$1`, source).Scan(&jobs); err != nil || jobs != 2 {
				t.Fatal("duplicate retry jobs", jobs, err)
			}
			mu.Lock()
			attempts := calls[execution]
			mu.Unlock()
			if attempts != 2 {
				t.Fatal("unexpected provider call count", attempts)
			}
			if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1 AND tenant_id=$2`, source, tenant).Scan(&matches); err != nil || matches != 1 {
				t.Fatal("recovery candidates", matches, err)
			}
		})
	}
	for _, tc := range []struct {
		name, status   string
		matches, links int
	}{
		{"Synthetic Example", "awaiting_review", 1, 1},
		{"Synthetic Empty", "no_hit", 0, 0},
		{"Synthetic Partial", "awaiting_review", 0, 1},
		{"Synthetic Late Acknowledgement", "awaiting_review", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluate(tc.name)
			var source, execution string
			waitFor(t, func() bool {
				return db.QueryRow(ctx, `SELECT s.id::text,e.id::text FROM core.screening_executions e
					JOIN screening.screenings s ON s.tenant_id=e.tenant_id AND s.idempotency_key=e.id::text
					WHERE e.decision_id=$1 AND e.status='completed' AND s.status=$2`, decision, tc.status).Scan(&source, &execution) == nil
			})
			if tc.name == "Synthetic Late Acknowledgement" {
				waitFor(t, func() bool {
					var rejected int
					return db.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind='screening_execution'
						AND args->>'execution_id'=$1 AND state='discarded'`, execution).Scan(&rejected) == nil && rejected == 1
				})
				mu.Lock()
				delayed := calls["late-ack:"+execution]
				mu.Unlock()
				if delayed != 1 {
					t.Fatal("late acknowledgement was not injected", delayed)
				}
			}
			var candidates int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1 AND tenant_id=$2`, source, tenant).Scan(&candidates); err != nil || candidates != tc.matches {
				t.Fatal("candidate persistence", candidates, err)
			}
			if tc.links == 0 {
				var arrivals int
				if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.case_event_outbox WHERE payload->>'screening_id'=$1 AND kind='requires-review'`, source).Scan(&arrivals); err != nil || arrivals != 0 {
					t.Fatal("no-hit incorrectly requested review", arrivals, err)
				}
			}
			waitFor(t, func() bool {
				var n int
				return db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_screenings s JOIN case_manager.cases c ON c.id=s.case_id
					WHERE s.screening_id=$1 AND c.tenant_id=$2 AND c.inbox_id=$3`, source, tenant, inbox).Scan(&n) == nil && n == tc.links
			})
			client := &http.Client{Timeout: 5 * time.Second}
			for i := 0; i < 3; i++ {
				req, _ := http.NewRequest("GET", decisionURL+"/v1/tenants/"+tenant+"/screening-executions/"+execution, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				if strings.HasPrefix(req.URL.String(), decisionURL+"/v1/") {
					req.Header.Set("X-Screening-Authoring-Token", operatorToken)
				}

				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					t.Fatal("execution polling", resp.StatusCode, err)
				}
				var body map[string]any
				if json.Unmarshal(raw, &body) != nil {
					t.Fatal("invalid execution response")
				}
				item, ok := body["screening_execution"].(map[string]any)
				if !ok || item["status"] != "completed" || item["id"] != execution {
					t.Fatal("poll did not return authoritative execution", body)
				}
			}
			// Raw source evidence requires private owner access and preserves tenant scoping.
			for _, read := range []struct {
				tenant   string
				operator bool
				want     int
			}{
				{tenant, false, 403}, {tenant, true, 200}, {"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", true, 404},
			} {
				req, _ := http.NewRequest("GET", screeningURL+"/v1/tenants/"+read.tenant+"/screenings/"+source, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				if read.operator {
					req.Header.Set("X-Screening-Operator-Token", sourceOperatorToken)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != read.want {
					t.Fatalf("source read got %d want %d", resp.StatusCode, read.want)
				}
			}
			// Replay the original dispatcher request after successful publication,
			// modeling lost HTTP acknowledgement without rewriting worker state.
			var payload []byte
			if err := db.QueryRow(ctx, `SELECT request_json FROM core.screening_executions WHERE id=$1 AND tenant_id=$2`, execution, tenant).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest("POST", screeningURL+"/internal/v1/tenants/"+tenant+"/decision-screenings", bytes.NewReader(payload))
			for _, deniedToken := range []string{token, testCallbackToken} {
				denied, _ := http.NewRequest("POST", req.URL.String(), bytes.NewReader(payload))
				denied.Header.Set("Authorization", "Bearer "+deniedToken)
				denied.Header.Set("Content-Type", "application/json")
				response, err := client.Do(denied)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 401 {
					t.Fatal("intake accepted wrong integration credential", response.StatusCode)
				}
			}
			req.Header.Set("Authorization", "Bearer "+testIntakeToken)
			if strings.HasPrefix(req.URL.String(), decisionURL+"/v1/") {
				req.Header.Set("X-Screening-Authoring-Token", operatorToken)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 201 {
				t.Fatal("intake replay failed", resp.StatusCode, err, string(raw))
			}
			var sources, jobs int
			if err := db.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM screening.screenings WHERE tenant_id=$1 AND idempotency_key=$2),
				(SELECT count(*) FROM river_job WHERE kind='screening_dispatch' AND args->>'screening_id'=$3)`,
				tenant, execution, source).Scan(&sources, &jobs); err != nil || sources != 1 || jobs != 1 {
				t.Fatal("intake replay duplicated source or job", sources, jobs, err)
			}
			mu.Lock()
			attempts := calls[execution]
			mu.Unlock()
			if attempts != 1 {
				t.Fatal("polling duplicated provider calls", attempts)
			}
			var outcome string
			if err := db.QueryRow(ctx, `SELECT outcome FROM core.decisions WHERE id=$1 AND tenant_id=$2`, decision, tenant).Scan(&outcome); err != nil || outcome != "approve" {
				t.Fatal("screening changed machine outcome", outcome, err)
			}
		})
	}
}

// Launch only against the disposable database, with an explicit local-only
// environment; never inherit provider credentials or deployment endpoints.
func startJourneyWorker(t *testing.T, db *pgxpool.Pool, binary string, environment ...string) {
	t.Helper()
	cfg := db.Config().ConnConfig
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), Path: "/" + cfg.Database, User: url.UserPassword(cfg.User, cfg.Password)}
	if cfg.TLSConfig == nil {
		u.RawQuery = "sslmode=disable"
	}
	dir := t.TempDir()
	cmd := exec.Command(binary)
	cmd.Dir = dir
	for _, key := range []string{"SystemRoot", "WINDIR", "PATH", "TEMP", "TMP", "USERPROFILE"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "DATABASE_URL="+u.String(), "SERVICE_AUTH_MODE=token", "SERVICE_AUTH_TOKEN=continuous-intake-integration-test-token-32-bytes",
		"DATA_MODEL_SERVICE_URL=http://127.0.0.1:1", "INGESTION_SERVICE_URL=http://127.0.0.1:1", "WORKER_MODE=poll", "WORKER_POLL_INTERVAL=100ms",
		"SCREENING_QUEUE_WORKERS=2", "SCREENING_DISPATCH_QUEUE_WORKERS=2")
	cmd.Env = append(cmd.Env, "SCREENING_INTAKE_TOKEN="+testIntakeToken, "SCREENING_CALLBACK_TOKEN="+testCallbackToken)
	cmd.Env = append(cmd.Env, environment...)
	path := filepath.Join(dir, "worker.log")
	log, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(path)
			t.Log(string(raw))
		}
	})
}
