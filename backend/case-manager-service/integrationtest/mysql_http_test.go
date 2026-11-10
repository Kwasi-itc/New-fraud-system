package integrationtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestMySQLCaseHTTP(t *testing.T) {
	f := fixture(t)
	t.Setenv("USER_REQUIRE_SESSION", "true")
	gin.SetMode(gin.TestMode)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	must(t, e)
	raw, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	must(t, e)
	verifier, e := access.NewVerifier(map[string][]byte{"test": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: raw})}, "case-test", "case-test")
	must(t, e)
	sign := func(tenant, user, session uuid.UUID, roles []string) string {
		claims := access.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "case-test", Subject: user.String(), Audience: jwt.ClaimStrings{"case-test"}, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, TenantID: tenant.String(), SessionID: session.String(), AuthzVersion: 1, Roles: roles}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "test"
		s, e := token.SignedString(key)
		must(t, e)
		return s
	}
	token := sign(f.tenant, f.user, f.sid, []string{"case_manager_admin"})
	r, e := httpapi.NewMySQLRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), f.db, f.database, httpapi.RouterConfig{UserVerifier: verifier, AuthMode: "token", AuthToken: "private-fixture-service-token", ServiceTenantIDs: []uuid.UUID{f.tenant}, EnableWorkflowReceipts: true})
	must(t, e)
	httpapi.MountMySQLDocs(r)
	positive := map[string]bool{}
	t.Cleanup(func() {
		if t.Failed() {
			return
		}
		var routes []string
		for key := range positive {
			routes = append(routes, key)
		}
		sort.Strings(routes)
		t.Logf("verified successful HTTP operations: %d of 61", len(routes))
		if dir := os.Getenv("CASE_TEST_REPORT_DIR"); dir != "" {
			must(t, os.MkdirAll(dir, 0755))
			report, e := json.MarshalIndent(map[string]any{"verified_at": time.Now().UTC(), "source_operations": 61, "successful_operations": routes, "authenticated_guard_operations": 59, "source_placeholders": 3}, "", "  ")
			must(t, e)
			must(t, os.WriteFile(filepath.Join(dir, "mysql-http-results.json"), report, 0644))
		}
	})
	call := func(t *testing.T, method, path, body string, want int, credential string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: status %d want %d body=%s", method, path, w.Code, want, w.Body.String())
		}
		if want >= 200 && want < 300 && path != "/docs" && path != "/openapi.json" {
			for _, route := range r.Routes() {
				if route.Method != method {
					continue
				}
				pattern := regexp.MustCompile(`:[^/]+`).ReplaceAllString(route.Path, `[^/]+`)
				if matched, _ := regexp.MatchString("^"+pattern+"$", strings.Split(path, "?")[0]); matched {
					positive[method+" "+route.Path] = true
					break
				}
			}
		}
		out := map[string]any{}
		if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			must(t, json.Unmarshal(w.Body.Bytes(), &out))
		}
		return out
	}
	getID := func(out map[string]any, key string) string {
		t.Helper()
		v, ok := out[key].(map[string]any)
		if !ok {
			t.Fatalf("missing %s: %+v", key, out)
		}
		s, ok := v["id"].(string)
		if !ok {
			t.Fatal("missing resource ID")
		}
		return s
	}
	prefix := "/v1/tenants/" + f.tenant.String()
	call(t, "GET", "/healthz", "", 200, "")
	call(t, "GET", "/readyz", "", 200, "")
	call(t, "GET", "/docs", "", 200, "")
	spec := call(t, "GET", "/openapi.json", "", 200, "")
	paths := spec["paths"].(map[string]any)
	operations := 0
	for _, v := range paths {
		operations += len(v.(map[string]any))
	}
	if operations != 61 {
		t.Fatalf("documented operations=%d", operations)
	}
	t.Run("every protected route requires authentication", func(t *testing.T) {
		for _, route := range r.Routes() {
			if !strings.HasPrefix(route.Path, "/v1/") && !strings.HasPrefix(route.Path, "/internal/") {
				continue
			}
			path := route.Path
			for _, segment := range strings.Split(path, "/") {
				if strings.HasPrefix(segment, ":") {
					path = strings.ReplaceAll(path, segment, uuid.NewString())
				}
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(route.Method, path, strings.NewReader(`{}`)))
			if w.Code != 401 {
				t.Errorf("%s %s: %d", route.Method, route.Path, w.Code)
			}
		}
	})
	call(t, "GET", "/v1/service-info", "", 200, token)
	inbox := getID(call(t, "POST", prefix+"/inboxes", `{"name":"High risk transfers"}`, 201, token), "inbox")
	ip := prefix + "/inboxes/" + inbox
	call(t, "GET", prefix+"/inboxes", "", 200, token)
	call(t, "GET", ip, "", 200, token)
	call(t, "PATCH", ip, `{"name":"Large payments","sla_days":2,"escalation_inbox_id":"`+f.inbox.String()+`"}`, 200, token)
	call(t, "PUT", ip+"/users/"+f.user.String(), `{"capacity":20,"auto_assign_enabled":true}`, 200, token)
	call(t, "GET", ip+"/users", "", 200, token)
	call(t, "GET", "/internal/v1/tenants/"+f.tenant.String()+"/inboxes/"+inbox, "", 200, "private-fixture-service-token")
	caseID := getID(call(t, "POST", prefix+"/cases", `{"name":"Large transfer investigation","inbox_id":"`+inbox+`","type":"decision","decision_ids":[]}`, 201, token), "case")
	cp := prefix + "/cases/" + caseID
	decision, scenario := seedCaseDecision(t, f)
	call(t, "POST", cp+"/decisions", `{"decision_id":"`+decision.String()+`"}`, 201, token)
	call(t, "POST", prefix+"/decision-case-memberships", `{"decision_ids":["`+decision.String()+`"]}`, 200, token)
	call(t, "GET", cp+"/evidence-access/decisions/"+decision.String(), "", 204, token)
	link := seedExplorerModel(t, f)
	repositories, e := store.NewRepositories(f.db, f.database)
	must(t, e)
	_, e = repositories.Explorer.Explore(f.ctx, f.tenant, c.DecisionReference{ID: decision, ScenarioID: scenario, ObjectType: "transactions", ObjectID: "synthetic-transfer-001"}, c.ExplorationRequest{DecisionID: decision, Connection: "link:" + link.String()})
	must(t, e)
	_, e = repositories.Explorer.Decisions(f.ctx, f.tenant, "transactions", "synthetic-transfer-001", f.user.String(), true, nil)
	must(t, e)
	explored := call(t, "POST", cp+"/explore", `{"decision_id":"`+decision.String()+`","connection":"link:`+link.String()+`"}`, 200, token)
	current := explored["current"].(map[string]any)["data"].(map[string]any)
	if current["amount"] != float64(75000) || current["private_note"] != nil || current["internal_private"] != nil {
		t.Fatalf("unsafe record projection: %+v", current)
	}
	if value, ok := current["nullable_flag"]; !ok || value != nil || current["source_ip"] != "192.0.2.17/24" {
		t.Fatalf("incorrect typed record projection: %+v", current)
	}
	if records := explored["records"].([]any); len(records) != 1 || records[0].(map[string]any)["object_id"] != "customer-001" {
		t.Fatalf("relationship traversal: %+v", records)
	}
	ipConnection := ""
	for _, value := range explored["connections"].([]any) {
		connection := value.(map[string]any)
		if connection["label"] == "Source address" {
			ipConnection = connection["id"].(string)
		}
	}
	if ipConnection == "" {
		t.Fatal("IP relationship was omitted")
	}
	ipResult := call(t, "POST", cp+"/explore", `{"decision_id":"`+decision.String()+`","connection":"`+ipConnection+`"}`, 200, token)
	if len(ipResult["records"].([]any)) != 1 {
		t.Fatal("IP relationship did not match all address components")
	}
	call(t, "POST", cp+"/explore", `{"decision_id":"`+decision.String()+`","path":[{"connection":"link:`+link.String()+`","object_id":"customer-001"}]}`, 200, token)
	call(t, "POST", cp+"/explore", `{"decision_id":"`+decision.String()+`","path":[{"connection":"link:`+link.String()+`","object_id":"unrelated-customer"}]}`, 404, token)
	attachDecision, _ := seedCaseDecision(t, f)
	call(t, "POST", cp+"/explore/attach", `{"decision_id":"`+decision.String()+`","target_decision_id":"`+attachDecision.String()+`"}`, 200, token)
	otherCase := getID(call(t, "POST", prefix+"/cases", `{"name":"Related investigation","inbox_id":"`+inbox+`","type":"decision"}`, 201, token), "case")
	call(t, "POST", cp+"/decision-transfers", `{"decision_id":"`+decision.String()+`","to_case_id":"`+otherCase+`","operation_id":"`+uuid.NewString()+`","reason":"Consolidate related investigations"}`, 204, token)
	bulk := call(t, "POST", prefix+"/case-bulk", `{"operation_id":"`+uuid.NewString()+`","case_ids":["`+otherCase+`"],"action":"close","outcome":"false_positive","comment":"Synthetic false alarm"}`, 200, token)
	results := bulk["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["success"] != true {
		t.Fatalf("bulk operation failed: %+v", results)
	}
	workflowDecision, workflowScenario := seedCaseDecision(t, f)
	workflowBody := `{"workflow_execution_id":"` + uuid.NewString() + `","tenant_id":"` + f.tenant.String() + `","decision_id":"` + workflowDecision.String() + `","scenario_id":"` + workflowScenario.String() + `","action_type":"create_case","action_config":{"inbox_id":"` + inbox + `","name":"Workflow fraud case"}}`
	call(t, "POST", "/internal/v1/workflow-actions", workflowBody, 200, "private-fixture-service-token")
	execution := uuid.New()
	payload := json.RawMessage(`{"execution_id":"` + execution.String() + `","tenant_id":"` + f.tenant.String() + `","decision_id":"` + decision.String() + `","scenario_id":"` + scenario.String() + `","action_type":"create_case","action_config":{"inbox_id":"` + inbox + `","name":"Receipt case"}}`)
	keyJSON, e := json.Marshal(struct {
		Kind                             string
		TenantID, ResourceID             uuid.UUID
		DomainAttempt, RedriveGeneration int
	}{"workflow_execution", f.tenant, execution, 1, 0})
	must(t, e)
	keyHash := sha256.Sum256(keyJSON)
	hash := sha256.Sum256(payload)
	receipt, e := json.Marshal(map[string]any{"idempotency_key": "decision-v1-" + hex.EncodeToString(keyHash[:]), "kind": "workflow_execution", "tenant_id": f.tenant.String(), "resource_id": execution.String(), "domain_attempt": 1, "redrive_generation": 0, "payload_sha256": hex.EncodeToString(hash[:]), "payload": payload})
	must(t, e)
	call(t, "POST", "/internal/v1/workflow-receipts/deliver", string(receipt), 200, "private-fixture-service-token")
	call(t, "POST", "/internal/v1/workflow-receipts/deliver", string(receipt), 200, "private-fixture-service-token")
	call(t, "POST", "/internal/v1/workflow-receipts/reconcile", string(receipt), 200, "private-fixture-service-token")
	call(t, "GET", prefix+"/cases", "", 200, token)
	call(t, "GET", cp, "", 200, token)
	call(t, "PATCH", cp, `{"name":"Review source of funds"}`, 200, token)
	call(t, "GET", prefix+"/case-session", "", 200, token)
	call(t, "GET", prefix+"/case-queue?limit=1", "", 200, token)
	call(t, "GET", cp+"/overview", "", 200, token)
	t.Run("inbox access and current authorization version", func(t *testing.T) {
		user, sid := uuid.New(), uuid.New()
		digest := make([]byte, 32)
		copy(digest, sid[:])
		for _, v := range []struct {
			q string
			a []any
		}{
			{"INSERT INTO `" + f.database + "`.identity_users VALUES(?,'Private investigator',TRUE)", []any{id(user)}},
			{"INSERT INTO `" + f.database + "`.identity_memberships(tenant_id,user_id,roles,active,version) VALUES(?,?,'case_investigator',TRUE,1)", []any{id(f.tenant), id(user)}},
			{"INSERT INTO `" + f.database + "`.identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,?,?,?,UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR))", []any{id(sid), digest, id(f.tenant), id(user)}},
		} {
			_, e := f.db.ExecContext(f.ctx, v.q, v.a...)
			must(t, e)
		}
		investigator := sign(f.tenant, user, sid, []string{"case_investigator"})
		call(t, "GET", cp, "", 403, investigator)
		call(t, "POST", prefix+"/inboxes", `{"name":"Unauthorized inbox"}`, 403, investigator)
		call(t, "PUT", ip+"/users/"+user.String(), `{"capacity":2}`, 403, investigator)
		call(t, "PUT", ip+"/users/"+user.String(), `{"capacity":2}`, 200, token)
		call(t, "GET", cp, "", 200, investigator)
		_, e := f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.identity_memberships SET version=version+1 WHERE tenant_id=? AND user_id=?", id(f.tenant), id(user))
		must(t, e)
		call(t, "GET", cp, "", 401, investigator)
	})
	call(t, "POST", cp+"/comments", `{"comment":"Review source and destination accounts."}`, 201, token)
	for _, kind := range []string{"events", "decisions", "screenings", "files"} {
		call(t, "GET", cp+"/links/"+kind, "", 200, token)
	}
	call(t, "GET", cp+"/events", "", 200, token)
	tag := getID(call(t, "POST", prefix+"/tags", `{"name":"High amount","target":"case","color":"red"}`, 201, token), "tag")
	call(t, "GET", prefix+"/tags", "", 200, token)
	call(t, "PATCH", prefix+"/tags/"+tag, `{"name":"High amount payment"}`, 200, token)
	call(t, "POST", cp+"/tags", `{"tag_id":"`+tag+`"}`, 204, token)
	call(t, "DELETE", cp+"/tags/"+tag, "", 204, token)
	call(t, "DELETE", prefix+"/tags/"+tag, "", 204, token)
	call(t, "POST", cp+"/assign", `{"assignee_id":"`+f.user.String()+`"}`, 204, token)
	call(t, "POST", cp+"/unassign", `{}`, 204, token)
	call(t, "POST", cp+"/snooze", `{"until":"`+time.Now().UTC().Add(time.Hour).Format(time.RFC3339)+`"}`, 204, token)
	call(t, "POST", cp+"/unsnooze", `{}`, 204, token)
	upload := getID(call(t, "POST", cp+"/uploads", `{"file_name":"evidence.txt","file_size":8,"content_type":"text/plain"}`, 201, token), "upload")
	call(t, "PUT", cp+"/uploads/"+upload+"/content", "evidence", 204, token)
	file := getID(call(t, "POST", cp+"/uploads/"+upload+"/finalize", `{}`, 200, token), "file")
	call(t, "GET", cp+"/files/"+file+"/download", "", 200, token)
	reportID := uuid.NewString()
	call(t, "POST", cp+"/reports", `{"id":"`+reportID+`","content":{"title":"Suspicious payment","subject":"Account A","narrative":"A large payment requires investigation.","activity_from":"2026-11-01T00:00:00Z","activity_to":"2026-11-02T00:00:00Z","file_ids":["`+upload+`"]}}`, 201, token)
	call(t, "GET", cp+"/reports", "", 200, token)
	call(t, "GET", cp+"/reports/"+reportID, "", 200, token)
	call(t, "PATCH", cp+"/reports/"+reportID, `{"version":1,"content":{"title":"Suspicious payment","subject":"Account A","narrative":"A large payment requires investigation.","activity_from":"2026-11-01T00:00:00Z","activity_to":"2026-11-02T00:00:00Z","file_ids":["`+upload+`"]}}`, 200, token)
	call(t, "POST", cp+"/reports/"+reportID+"/complete", `{"version":2}`, 200, token)
	call(t, "GET", cp+"/reports/"+reportID+"/export", "", 200, token)
	call(t, "GET", prefix+"/case-analytics?from="+time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)+"&to="+time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "", 200, token)
	call(t, "GET", "/v1/tenants/"+f.other.String()+"/cases/"+caseID, "", 403, token)
	call(t, "POST", cp+"/comments", `{"comment":""}`, 400, token)
	t.Run("unavailable screening owner contracts fail without mutations", func(t *testing.T) {
		var before, after int
		must(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM `"+f.database+"`.case_manager_case_events WHERE tenant_id=?", id(f.tenant)).Scan(&before))
		call(t, "POST", prefix+"/manual-screening-investigations", `{"screening_id":"`+uuid.NewString()+`","candidate_id":"`+uuid.NewString()+`","inbox_id":"`+inbox+`"}`, 503, token)
		call(t, "POST", cp+"/files", `{"source_file_id":"`+uuid.NewString()+`"}`, 503, token)
		screening := uuid.New()
		event := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+f.tenant.String()+"/"+screening.String()))
		call(t, "POST", "/v1/screening-events/requires-review", `{"version":1,"tenant_id":"`+f.tenant.String()+`","screening_id":"`+screening.String()+`","event_id":"`+event.String()+`"}`, 503, "private-fixture-service-token")
		call(t, "POST", "/v1/screening-events/reviewed", `{"event_id":"`+uuid.NewString()+`","tenant_id":"`+f.tenant.String()+`","screening_id":"`+uuid.NewString()+`","match_id":"`+uuid.NewString()+`","status":"no_hit"}`, 404, "private-fixture-service-token")
		must(t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM `"+f.database+"`.case_manager_case_events WHERE tenant_id=?", id(f.tenant)).Scan(&after))
		if before != after {
			t.Fatal("unavailable contract created an audit event")
		}
	})
	call(t, "PUT", prefix+"/inboxes/"+f.inbox.String()+"/users/"+f.user.String(), `{"capacity":20}`, 200, token)
	call(t, "POST", cp+"/escalate", `{}`, 204, token)
	call(t, "POST", cp+"/close", `{"outcome":"confirmed_risk","comment":"Confirmed suspicious activity."}`, 200, token)
	call(t, "POST", cp+"/unassign", `{}`, 204, token)
	call(t, "DELETE", ip+"/users/"+f.user.String(), "", 204, token)
	call(t, "POST", "/internal/v1/auto-assignment/run", `{}`, 501, "private-fixture-service-token")
	call(t, "POST", "/internal/v1/ai-case-reviews/"+uuid.NewString()+"/run", `{}`, 501, "private-fixture-service-token")
	call(t, "POST", "/v1/screening-events/evidence-uploaded", `{"tenant_id":"`+f.tenant.String()+`","screening_id":"`+uuid.NewString()+`","file_id":"`+uuid.NewString()+`"}`, 400, "private-fixture-service-token")
}
