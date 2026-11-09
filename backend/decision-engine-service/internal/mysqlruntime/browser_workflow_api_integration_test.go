package mysqlruntime

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func testBrowserWorkflowAPIs(t *testing.T, h http.Handler, base string, db *sql.DB, schema string, session *atomic.Pointer[mysql.TrustedDecisionSession]) {
	t.Helper()
	defer session.Store(nil)
	request := func(method, path, body string, auth bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		// These deliberately forged headers must never become the audit actor.
		req.Header.Set("X-Workflow-User", uuid.NewString())
		req.Header.Set("X-Workflow-Session", uuid.NewString())
		req.Header.Set("X-Workflow-Version", "999")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: got %d want %d %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	encode := func(v any) string {
		t.Helper()
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	var created struct {
		Scenario struct {
			ID string `json:"id"`
		} `json:"scenario"`
	}
	rec := request("POST", base+"/scenarios", `{"name":"Browser API fixture","description":"fixture","trigger_object_type":"wallets"}`, true, 201)
	if e := json.Unmarshal(rec.Body.Bytes(), &created); e != nil || created.Scenario.ID == "" {
		t.Fatal(e, rec.Body.String())
	}
	parent := created.Scenario.ID
	tenant := uuid.MustParse(strings.TrimPrefix(base, "/v1/tenants/"))
	path := "/internal/workflow-authoring/tenants/" + tenant.String() + "/scenarios/" + parent
	rule := workflow.CaseRule{ID: uuid.NewString(), Name: "Browser rule", Enabled: true, ActionType: workflow.ActionTypeDoNothing, Condition: &workflow.ConditionTree{Kind: "always"}, Outcomes: []string{}, TagIDs: []string{}}
	target := path + "/rules/" + rule.ID
	t.Run("trusted session required", func(t *testing.T) {
		request("GET", path, "", false, 401)
		request("GET", path, "", true, 503)
		request("PUT", target, encode(rule), true, 503)
		request("GET", strings.Replace(path, tenant.String(), uuid.NewString(), 1), "", true, 403)
	})
	actor := mysql.TrustedDecisionSession{TenantID: tenant, ActorID: uuid.New(), SessionID: uuid.New(), MembershipVersion: 7}
	session.Store(&actor)
	request("GET", path, "", true, 503) // no real/fixture providers installed yet
	// Explicit disposable contract fixtures, not production identity/case owners.
	exec("CREATE TABLE " + schema + ".fixture_http_browser_permission(id INT PRIMARY KEY,allowed BOOLEAN NOT NULL)")
	exec("INSERT INTO " + schema + ".fixture_http_browser_permission VALUES(1,1)")
	identity := fmt.Sprintf("a=UNHEX(REPLACE('%s','-','')) AND t=UNHEX(REPLACE('%s','-','')) AND s=UNHEX(REPLACE('%s','-','')) AND v=7 AND EXISTS(SELECT 1 FROM %s.fixture_http_browser_permission WHERE id=1 AND allowed=1)", actor.ActorID, tenant, actor.SessionID, schema)
	exec("CREATE FUNCTION " + schema + ".identity_authorize_decision_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,act VARCHAR(32),locks BOOLEAN) RETURNS BOOLEAN READS SQL DATA RETURN " + identity + " AND act IN('decision.workflow.author','decision.workflow.read') AND locks=(act='decision.workflow.author')")
	exec("CREATE FUNCTION " + schema + ".case_manager_authorize_decision_inboxes_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,ids JSON,active BOOLEAN,locks BOOLEAN) RETURNS BOOLEAN READS SQL DATA RETURN " + identity + " AND JSON_LENGTH(ids)=0 AND locks=1")
	exec("CREATE FUNCTION " + schema + ".case_manager_authorize_decision_tags_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,ids JSON,active BOOLEAN,locks BOOLEAN) RETURNS BOOLEAN READS SQL DATA RETURN " + identity + " AND JSON_LENGTH(ids)=0 AND locks=1")
	exec("CREATE FUNCTION " + schema + ".case_manager_visible_decision_inboxes_v1(a BINARY(16),t BINARY(16),s BINARY(16),v BIGINT,ids JSON) RETURNS JSON READS SQL DATA RETURN IF(" + identity + " AND JSON_LENGTH(ids)=0,JSON_ARRAY(),NULL)")
	var saved workflow.CaseRule
	t.Run("list save replay and trusted audit actor", func(t *testing.T) {
		rec := request("PUT", target, encode(rule), true, 200)
		if e := json.Unmarshal(rec.Body.Bytes(), &saved); e != nil || saved.UpdatedAt == nil {
			t.Fatal(e, rec.Body.String())
		}
		request("PUT", target, encode(rule), true, 200)
		var auditActor mysqlstore.UUID
		var n int
		if e := db.QueryRow("SELECT actor_id FROM "+schema+".decision_engine_workflow_authoring_events WHERE tenant_id=? AND rule_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(rule.ID))).Scan(&auditActor); e != nil || uuid.UUID(auditActor) != actor.ActorID {
			t.Fatal(auditActor, e)
		}
		if e := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_workflow_authoring_events WHERE tenant_id=? AND rule_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(rule.ID))).Scan(&n); e != nil || n != 1 {
			t.Fatal(n, e)
		}
		rec = request("GET", path, "", true, 200)
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private response cacheable")
		}
		var page workflow.AuthoringPage
		if e := json.Unmarshal(rec.Body.Bytes(), &page); e != nil || len(page.Rules) != 1 || page.Rules[0].ID != saved.ID {
			t.Fatal(page, e)
		}
	})
	t.Run("strict input and stale writes", func(t *testing.T) {
		request("GET", path+"?actor=forged", "", true, 400)
		request("PUT", target, strings.TrimSuffix(encode(rule), "}")+`,"actor_id":"forged"}`, true, 400)
		request("PUT", target, strings.Replace(encode(rule), rule.ID, uuid.NewString(), 1), true, 400)
		bad := rule
		bad.Name = ""
		request("PUT", target, encode(bad), true, 400)
		bad = rule
		bad.Name = "stale"
		request("PUT", target, encode(bad), true, 409)
		request("DELETE", target, `{"id":"`+rule.ID+`"}`, true, 400)
		request("GET", strings.Replace(path, parent, uuid.NewString(), 1), "", true, 404)
	})
	t.Run("revocation and read only", func(t *testing.T) {
		exec("UPDATE " + schema + ".fixture_http_browser_permission SET allowed=0")
		request("GET", path, "", true, 403)
		request("PUT", target, encode(saved), true, 403)
		exec("UPDATE " + schema + ".fixture_http_browser_permission SET allowed=1")
		readonly := actor
		readonly.ReadOnly = true
		session.Store(&readonly)
		request("GET", path, "", true, 200)
		request("PUT", target, encode(saved), true, 403)
		session.Store(&actor)
	})
	t.Run("audit failure rolls back update", func(t *testing.T) {
		exec("CREATE TRIGGER " + schema + ".fixture_browser_audit_fault BEFORE INSERT ON " + schema + ".decision_engine_workflow_authoring_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private audit failure'")
		defer exec("DROP TRIGGER " + schema + ".fixture_browser_audit_fault")
		changed := saved
		changed.Name = "must rollback"
		rec := request("PUT", target, encode(changed), true, 500)
		if strings.Contains(rec.Body.String(), "private audit failure") {
			t.Fatal("SQL error exposed")
		}
		var page workflow.AuthoringPage
		rec = request("GET", path, "", true, 200)
		if e := json.Unmarshal(rec.Body.Bytes(), &page); e != nil || len(page.Rules) != 1 || page.Rules[0].Name != saved.Name {
			t.Fatal(page, e)
		}
	})
	t.Run("copy validates destination and writes attributed audit atomically", func(t *testing.T) {
		var destination struct {
			Scenario struct {
				ID string `json:"id"`
			} `json:"scenario"`
		}
		rec := request("POST", base+"/scenarios", `{"name":"Browser copy destination","description":"fixture","trigger_object_type":"wallets"}`, true, 201)
		if err := json.Unmarshal(rec.Body.Bytes(), &destination); err != nil || destination.Scenario.ID == "" {
			t.Fatal(err, rec.Body.String())
		}
		body := encode(map[string]string{"destination_scenario_id": destination.Scenario.ID})
		request("POST", path+"/copy", body, false, 401)
		request("POST", path+"/copy", `{}`, true, 400)
		request("POST", path+"/copy", encode(map[string]string{"destination_scenario_id": parent}), true, 400)
		request("POST", path+"/copy", encode(map[string]string{"destination_scenario_id": uuid.NewString()}), true, 404)
		exec("UPDATE " + schema + ".fixture_http_browser_permission SET allowed=0")
		request("POST", path+"/copy", body, true, 403)
		exec("UPDATE " + schema + ".fixture_http_browser_permission SET allowed=1")
		exec("CREATE TRIGGER " + schema + ".fixture_browser_copy_audit_fault BEFORE INSERT ON " + schema + ".decision_engine_workflow_authoring_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private copy failure'")
		request("POST", path+"/copy", body, true, 500)
		exec("DROP TRIGGER " + schema + ".fixture_browser_copy_audit_fault")
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_workflow_rules WHERE tenant_id=? AND scenario_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(destination.Scenario.ID))).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed copy persisted", count, err)
		}
		rec = request("POST", path+"/copy", body, true, 200)
		var result struct {
			Rules []workflow.CaseRule `json:"rules"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || len(result.Rules) != 1 || result.Rules[0].ID == saved.ID {
			t.Fatal(result, err)
		}
		var recorded mysqlstore.UUID
		if err := db.QueryRow("SELECT actor_id FROM "+schema+".decision_engine_workflow_authoring_events WHERE tenant_id=? AND scenario_id=? AND rule_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(destination.Scenario.ID)), mysqlstore.UUID(uuid.MustParse(result.Rules[0].ID))).Scan(&recorded); err != nil || uuid.UUID(recorded) != actor.ActorID {
			t.Fatal(recorded, err)
		}
		exec("UPDATE "+schema+".decision_engine_scenarios SET trigger_object_type='unpublished' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(uuid.MustParse(destination.Scenario.ID)))
		request("POST", path+"/copy", body, true, 409)
	})
	t.Run("versioned delete", func(t *testing.T) {
		request("DELETE", target, encode(saved), true, 200)
		var page workflow.AuthoringPage
		rec := request("GET", path, "", true, 200)
		if e := json.Unmarshal(rec.Body.Bytes(), &page); e != nil || len(page.Rules) != 0 {
			t.Fatal(page, e)
		}
	})
}
