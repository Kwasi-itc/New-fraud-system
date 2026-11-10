package integrationtest

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	api "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	runtime "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	native "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func seedDecision(t *testing.T, f *fixture) (decision, scenario, config, execution uuid.UUID) {
	t.Helper()
	if !strings.HasPrefix(f.database, "screen_fixture_") {
		t.Fatal("private fixture required")
	}
	decision, scenario, config, execution = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	iteration := uuid.New()
	now := time.Now().UTC()
	schema := "`" + f.database + "`."
	rows := []struct {
		q string
		a []any
	}{
		{"INSERT INTO " + schema + "decision_engine_scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES(?,?,'Synthetic screening scenario','Person',?,?)", []any{mysqlstore.UUID(scenario), mysqlstore.UUID(f.tenant), now, now}},
		{"INSERT INTO " + schema + "decision_engine_scenario_iterations(id,tenant_id,scenario_id,version,status,created_at) VALUES(?,?,?,1,'draft',?)", []any{mysqlstore.UUID(iteration), mysqlstore.UUID(f.tenant), mysqlstore.UUID(scenario), now}},
		{"INSERT INTO " + schema + "decision_engine_decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,score,triggered,created_at) VALUES(?,?,?,?,'screen-person-001','Person','review',85,TRUE,?)", []any{mysqlstore.UUID(decision), mysqlstore.UUID(f.tenant), mysqlstore.UUID(scenario), mysqlstore.UUID(iteration), now}},
		{"INSERT INTO " + schema + "decision_engine_screening_configs(id,tenant_id,scenario_id,name,provider,config_json,active,review_inbox_id,created_at,updated_at) VALUES(?,?,?,'Synthetic name check','opensanctions',JSON_OBJECT(),TRUE,?,?,?)", []any{mysqlstore.UUID(config), mysqlstore.UUID(f.tenant), mysqlstore.UUID(scenario), mysqlstore.UUID(f.inbox), now, now}},
		{"INSERT INTO " + schema + "decision_engine_screening_executions(id,tenant_id,config_id,decision_id,scenario_id,decision_iteration_id,status,request_json,provider_reference,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,'pending',JSON_OBJECT(),'','',?,?)", []any{mysqlstore.UUID(execution), mysqlstore.UUID(f.tenant), mysqlstore.UUID(config), mysqlstore.UUID(decision), mysqlstore.UUID(scenario), mysqlstore.UUID(iteration), now, now}},
	}
	for _, row := range rows {
		_, e := f.db.ExecContext(f.ctx, row.q, row.a...)
		must(t, e)
	}
	return
}
func tokenFixture(t *testing.T, f *fixture) (*identity.Verifier, string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	must(t, e)
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	must(t, e)
	verifier, e := identity.NewVerifier(map[string][]byte{"fixture": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})}, "http://auth-fixture", "screening-fixture")
	must(t, e)
	claims := jwt.MapClaims{"iss": "http://auth-fixture", "aud": "screening-fixture", "sub": f.user.String(), "tenant_id": f.tenant.String(), "roles": []string{"case_manager_admin", "platform_admin"}, "sid": f.sid.String(), "authz_version": 1, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "fixture"
	raw, e := token.SignedString(key)
	must(t, e)
	return verifier, raw
}
func routeKey(routes gin.RoutesInfo, method, path string) string {
	for _, r := range routes {
		if r.Method != method {
			continue
		}
		a, b := strings.Split(r.Path, "/"), strings.Split(path, "/")
		if len(a) != len(b) {
			continue
		}
		match := true
		for i := range a {
			if !strings.HasPrefix(a[i], ":") && a[i] != b[i] {
				match = false
				break
			}
		}
		if match {
			return method + " " + r.Path
		}
	}
	return ""
}
func TestMySQLScreeningHTTPWorkflows(t *testing.T) {
	f := fixtureMySQL(t)
	verifier, token := tokenFixture(t, f)
	apiDB, workerDB := f.runtimeDB(t, "api"), f.runtimeDB(t, "worker")
	apiStore, e := native.New(apiDB, f.database)
	must(t, e)
	workerStore, e := native.New(workerDB, f.database)
	must(t, e)
	blob, e := runtime.NewLocalBlob(apiDB, f.database, "http://localhost:8085")
	must(t, e)
	cfg := api.RouterConfig{AuthMode: "token", AuthToken: strings.Repeat("a", 64), ScreeningOperatorToken: strings.Repeat("b", 64), ScreeningIntakeToken: strings.Repeat("c", 64), ScreeningCallbackToken: strings.Repeat("d", 64), InvestigatorVerifier: verifier}
	deps := api.MySQLDependencies{Provider: runtime.LocalProvider{}, Capabilities: runtime.LocalCapabilities(), Inbox: apiStore, Blob: blob}
	router, e := api.NewMySQLRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), apiDB, f.database, cfg, deps)
	must(t, e)
	routes := router.Routes()
	positive := map[string]bool{}
	call := func(method, path string, body any, auth string, expected int) []byte {
		t.Helper()
		var input []byte
		switch v := body.(type) {
		case nil:
		case []byte:
			input = v
		default:
			var e error
			input, e = json.Marshal(body)
			must(t, e)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(input))
		req.Header.Set("Content-Type", "application/json")
		if auth == "owner" {
			req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
			req.Header.Set("X-Screening-Operator-Token", cfg.ScreeningOperatorToken)
		} else if auth == "intake" {
			req.Header.Set("Authorization", "Bearer "+cfg.ScreeningIntakeToken)
		} else if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != expected {
			t.Fatalf("%s %s: wanted %d, got %d: %s", method, path, expected, rec.Code, rec.Body.String())
		}
		if expected >= 200 && expected < 300 {
			u, _ := url.Parse(path)
			positive[routeKey(routes, method, u.Path)] = true
		}
		return rec.Body.Bytes()
	}
	for _, path := range []string{"/healthz", "/readyz", "/metrics", "/docs", "/redoc", "/openapi.yaml", "/openapi.json"} {
		call("GET", path, nil, "", 200)
	}
	call("GET", "/v1/service-info", nil, "owner", 200)
	call("GET", "/v1/screening-provider/catalog", nil, "owner", 200)
	call("GET", "/v1/screening-provider/freshness", nil, "owner", 200)
	call("GET", "/internal/screening-capabilities", nil, "intake", 200)
	call("GET", "/v1/manual-screenings/capabilities", nil, token, 200)
	var started struct{ ID, Status string }
	must(t, json.Unmarshal(call("POST", "/v1/manual-screenings", f.manualInput(), token, 202), &started))
	svc := api.MySQLService(f.store, deps)
	dispatch := service.NewDispatchService(workerStore, clock{}, workerStore.Screenings(), workerStore.ScreeningMatches(), deps.Provider, nil)
	must(t, dispatch.RunScreening(f.ctx, f.tenant.String(), started.ID))
	call("GET", "/v1/manual-screenings", nil, token, 200)
	call("GET", "/v1/manual-screenings/"+started.ID, nil, token, 200)
	details, e := svc.GetDetails(f.ctx, f.tenant.String(), started.ID)
	must(t, e)
	if len(details.Matches) != 1 {
		t.Fatal("expected persisted synthetic match")
	}
	match := details.Matches[0]
	base := "/v1/tenants/" + f.tenant.String()
	screen := base + "/screenings/" + started.ID
	call("GET", screen, nil, "owner", 200)
	create := map[string]any{"provider": "opensanctions", "object_type": "Person", "object_id": "owner-person-001", "queries": []map[string]string{{"name": "Alex Example", "type": "Person"}}, "provider_config": map[string]int{"threshold": 90}}
	var owner d.Screening
	must(t, json.Unmarshal(call("POST", base+"/screenings", create, "owner", 201), &owner))
	call("POST", base+"/screenings/freeform-search", create, "owner", 201)
	call("POST", base+"/screening-matches/"+match.ID+"/comments", map[string]any{"comment": "Synthetic evidence checked", "author_id": f.user.String()}, "owner", 201)
	call("POST", base+"/screening-matches/"+match.ID+"/enrich", nil, "owner", 200)
	// Human review requires actual case linkage; its receipt and case history
	// commit in the same transaction as the screening match and parent status.
	caseID := uuid.New()
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.case_manager_cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES(?,?,?,'Synthetic manual investigation','pending','unset','manual_screening',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(caseID), mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.inbox))
	must(t, e)
	_, e = f.db.ExecContext(f.ctx, "INSERT INTO `"+f.database+"`.case_manager_case_screenings(id,tenant_id,case_id,screening_id,match_id,status,created_at) VALUES(?,?,?,?,?,'pending',UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(f.tenant), mysqlstore.UUID(caseID), mysqlstore.UUID(uuid.MustParse(started.ID)), mysqlstore.UUID(uuid.MustParse(match.ID)))
	must(t, e)
	reviewPath := "/v1/investigator/tenants/" + f.tenant.String() + "/cases/" + caseID.String() + "/review"
	var state identity.State
	must(t, json.Unmarshal(call("GET", reviewPath+"?resource_id="+match.ID, nil, token, 200), &state))
	if !state.Eligible {
		t.Fatal("linked pending match is not reviewable")
	}
	cmd := identity.Command{OperationID: uuid.New(), ResourceID: match.ID, ExpectedVersion: state.Version, Action: "confirmed_hit", Findings: "Synthetic investigator confirmation"}
	call("POST", reviewPath, cmd, token, 200)
	call("POST", reviewPath, cmd, token, 200)
	call("GET", strings.TrimSuffix(reviewPath, "/review")+"/exclusions", nil, token, 200)
	call("POST", base+"/screening-matches/"+match.ID+"/review", map[string]any{"status": "no_hit", "comment": "Private operator follow-up", "reviewer_id": f.user.String()}, "owner", 200)
	call("POST", base+"/screening-whitelist", map[string]string{"entity_id": "local-person-001", "reviewer_id": f.user.String()}, "owner", 201)
	call("GET", base+"/screening-whitelist?entity_id=local-person-001", nil, "owner", 200)
	call("DELETE", base+"/screening-whitelist?entity_id=local-person-001", nil, "owner", 204)
	upload := map[string]any{"file_name": "synthetic-note.txt", "content_type": "text/plain", "file_size": 4, "uploaded_by": f.user.String()}
	var fileSession struct {
		File    d.File                                         `json:"file"`
		Session struct{ StorageKey, UploadURL, Method string } `json:"session"`
	}
	raw := call("POST", screen+"/file-uploads", upload, "owner", 201)
	var generic struct {
		File    d.File            `json:"file"`
		Session map[string]string `json:"session"`
	}
	must(t, json.Unmarshal(raw, &generic))
	fileSession.File = generic.File
	fileSession.Session.StorageKey = generic.Session["storage_key"]
	uploadURL, _ := url.Parse(generic.Session["upload_url"])
	req := httptest.NewRequest("PUT", uploadURL.Path, strings.NewReader("note"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("capability upload failed: %d %s", rec.Code, rec.Body.String())
	}
	positive["PUT /local/evidence/:capability"] = true
	var download struct {
		URL string `json:"download_url"`
	}
	must(t, json.Unmarshal(call("GET", screen+"/files/"+generic.File.ID+"/download", nil, "owner", 200), &download))
	downloadURL, _ := url.Parse(download.URL)
	if string(call("GET", downloadURL.Path, nil, "", 200)) != "note" {
		t.Fatal("downloaded evidence bytes differ")
	}
	call("POST", screen+"/files", map[string]any{"file_name": "synthetic-note.txt", "content_type": "text/plain", "file_size": 4, "uploaded_by": f.user.String(), "storage_key": generic.Session["storage_key"]}, "owner", 201)
	call("GET", screen+"/files", nil, "owner", 200)
	call("GET", screen+"/files/"+generic.File.ID, nil, "owner", 200)
	configBody := map[string]any{"name": "Synthetic ongoing check", "object_type": "Person", "provider": "opensanctions", "field_map": map[string]string{"name": "Person"}, "review_inbox_id": f.inbox.String(), "enabled": true}
	var config d.ContinuousConfig
	must(t, json.Unmarshal(call("POST", base+"/continuous-screening-configs", configBody, "owner", 201), &config))
	configPath := base + "/continuous-screening-configs/" + config.ID
	call("GET", base+"/continuous-screening-configs", nil, "owner", 200)
	call("GET", configPath, nil, "owner", 200)
	call("PUT", configPath, configBody, "owner", 200)
	var monitored d.MonitoredObject
	must(t, json.Unmarshal(call("POST", configPath+"/monitored-objects", map[string]any{"object_type": "Person", "object_id": "monitored-person-001", "attributes": map[string]string{"source": "synthetic"}}, "owner", 201), &monitored))
	call("GET", configPath+"/monitored-objects", nil, "owner", 200)
	monitoredPath := base + "/monitored-objects/" + monitored.ID
	call("GET", monitoredPath, nil, "owner", 200)
	call("POST", monitoredPath+"/requeue", map[string]any{}, "owner", 200)
	call("DELETE", monitoredPath, nil, "owner", 204)
	call("DELETE", configPath, nil, "owner", 204)
	var job d.DatasetUpdateJob
	must(t, json.Unmarshal(call("POST", base+"/dataset-update-jobs", map[string]string{"provider": "opensanctions", "job_type": "catalog_sync"}, "owner", 201), &job))
	call("GET", base+"/dataset-update-jobs", nil, "owner", 200)
	call("GET", base+"/dataset-update-jobs/"+job.ID, nil, "owner", 200)
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_dataset_jobs SET status='failed',last_error='synthetic injected failure' WHERE id=UUID_TO_BIN(?)", job.ID)
	must(t, e)
	call("POST", base+"/dataset-update-jobs/"+job.ID+"/retry", nil, "owner", 200)
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_screenings SET status='failed',projection_version=projection_version+1 WHERE id=UUID_TO_BIN(?)", owner.ID)
	must(t, e)
	call("POST", base+"/screenings/"+owner.ID+"/retry", nil, "owner", 200)
	decision, scenario, decisionConfig, execution := seedDecision(t, f)
	linked := map[string]any{"provider": "opensanctions", "decision_id": decision, "scenario_id": scenario, "screening_config_id": decisionConfig, "idempotency_key": execution.String(), "object_type": "Person", "object_id": "screen-person-001", "queries": []map[string]string{{"name": "Alex Example", "type": "Person"}}}
	var linkedSource d.Screening
	must(t, json.Unmarshal(call("POST", "/internal/v1/tenants/"+f.tenant.String()+"/decision-screenings", linked, "intake", 201), &linkedSource))
	call("GET", base+"/decisions/"+decision.String()+"/screenings", nil, "owner", 200)
	must(t, dispatch.RunScreening(f.ctx, f.tenant.String(), linkedSource.ID))
	must(t, runtime.ProjectBatch(f.ctx, workerDB, f.database))
	var projected string
	must(t, f.db.QueryRowContext(f.ctx, "SELECT status FROM `"+f.database+"`.decision_engine_screening_executions WHERE id=UUID_TO_BIN(?)", execution.String()).Scan(&projected))
	if projected != "completed" {
		t.Fatalf("decision owner projection missing: %s", projected)
	}
	// Every protected source operation must reject a request without credentials.
	guarded := 0
	for _, r := range routes {
		if !strings.HasPrefix(r.Path, "/v1/") && !strings.HasPrefix(r.Path, "/internal/") {
			continue
		}
		path := r.Path
		for _, part := range strings.Split(path, "/") {
			if strings.HasPrefix(part, ":") {
				value := uuid.NewString()
				if part == ":tenantId" {
					value = f.tenant.String()
				}
				path = strings.ReplaceAll(path, part, value)
			}
		}
		req := httptest.NewRequest(r.Method, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 401 && rec.Code != 403 {
			t.Fatalf("unguarded route %s %s: %d", r.Method, r.Path, rec.Code)
		}
		guarded++
	}
	call("GET", "/v1/tenants/"+f.other.String()+"/screenings/"+started.ID, nil, "owner", 404)
	call("GET", "/v1/tenants/"+uuid.NewString()+"/screenings/"+started.ID, nil, "owner", 403)
	call("GET", "/v1/investigator/tenants/"+f.other.String()+"/cases/"+caseID.String()+"/review?resource_id="+match.ID, nil, token, 403)
	call("GET", "/v1/manual-screenings", nil, "forged", 401)
	missing := []string{}
	keys := []string{}
	for _, r := range routes {
		key := r.Method + " " + r.Path
		if !positive[key] {
			missing = append(missing, key)
		} else {
			keys = append(keys, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("positive native HTTP gaps: %v", missing)
	}
	sort.Strings(keys)
	if output := os.Getenv("SCREENING_TEST_REPORT_DIR"); output != "" {
		must(t, os.MkdirAll(output, 0755))
		b, _ := json.MarshalIndent(map[string]any{"passed": true, "restricted_runtime_accounts": true, "successful_operations": keys, "protected_operations": guarded, "mounted_operations": len(routes), "verified_at": time.Now().UTC().Format(time.RFC3339)}, "", "  ")
		must(t, os.WriteFile(filepath.Join(output, "mysql-http-results.json"), append(b, '\n'), 0644))
	}
}
