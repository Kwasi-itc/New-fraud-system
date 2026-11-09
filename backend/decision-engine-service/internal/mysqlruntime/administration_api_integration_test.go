package mysqlruntime

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func testAdministrationAPIs(t *testing.T, h http.Handler, base string, db *sql.DB, schema string, revoked *atomic.Bool) {
	request := func(t *testing.T, method, path, body string, auth bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}
	id := func(t *testing.T, rec *httptest.ResponseRecorder, key string) string {
		t.Helper()
		var out map[string]struct {
			ID string `json:"id"`
		}
		if e := json.Unmarshal(rec.Body.Bytes(), &out); e != nil || out[key].ID == "" {
			t.Fatal(rec.Body.String(), e)
		}
		return out[key].ID
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	security := func(t *testing.T, method, path, body string) {
		t.Helper()
		request(t, method, path, body, false, 401)
		foreign := strings.Replace(path, base, "/v1/tenants/"+uuid.NewString(), 1)
		request(t, method, foreign, body, true, 403)
		revoked.Store(true)
		defer revoked.Store(false)
		request(t, method, path, body, true, 403)
	}
	parent := id(t, request(t, "POST", base+"/scenarios", `{"name":"Administration fixture","trigger_object_type":"wallets"}`, true, 201), "scenario")
	other := id(t, request(t, "POST", base+"/scenarios", `{"name":"Other administration scope","trigger_object_type":"wallets"}`, true, 201), "scenario")
	path := base + "/scenarios/" + parent
	workflows := path + "/workflows"
	body := `{"name":"Emit review","description":"history","allowed_outcomes":["review","review",""],"action_type":"emit_event","action_config":{"url":"https://example.test/event"},"active":true}`
	var first string
	t.Run("T23 workflow create", func(t *testing.T) {
		security(t, "POST", workflows, body)
		request(t, "POST", base+"/scenarios/"+uuid.NewString()+"/workflows", body, true, 404)
		request(t, "POST", workflows, strings.Replace(body, "Emit review", `Invalid\u0000name`, 1), true, 422)
		request(t, "POST", workflows, `{"name":"bad","allowed_outcomes":[null]}`, true, 400)
		request(t, "POST", workflows, `{"name":"bad","allowed_outcomes":["review"],"action_type":"emit_event","action_config":{}}`, true, 422)
		caseBody := `{"name":"case","allowed_outcomes":["review"],"action_type":"create_case","action_config":{"inbox_id":"` + uuid.NewString() + `"},"active":true}`
		request(t, "POST", workflows, caseBody, true, 503)
		exec("CREATE TRIGGER " + schema + ".fixture_admin_outcomes BEFORE INSERT ON " + schema + ".decision_engine_workflow_outcomes FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private admin fault'")
		rec := request(t, "POST", workflows, body, true, 500)
		if strings.Contains(rec.Body.String(), "private admin") {
			t.Fatal("SQL exposed")
		}
		exec("DROP TRIGGER " + schema + ".fixture_admin_outcomes")
		first = id(t, request(t, "POST", workflows, body, true, 201), "workflow")
	})
	if first == "" {
		t.Fatal("workflow create prerequisite failed")
	}
	t.Run("T21 workflow list", func(t *testing.T) {
		security(t, "GET", workflows, "")
		rec := request(t, "GET", workflows, "", true, 200)
		var out struct {
			Items []dto.WorkflowResponse `json:"workflows"`
		}
		if e := json.Unmarshal(rec.Body.Bytes(), &out); e != nil || len(out.Items) != 1 || out.Items[0].ID != first || out.Items[0].DisplayOrder != 0 || len(out.Items[0].AllowedOutcomes) != 3 {
			t.Fatal(rec.Body.String(), e)
		}
		request(t, "GET", workflows+"?ignore=1", "", true, 400)
	})
	t.Run("T22 workflow detail", func(t *testing.T) {
		security(t, "GET", workflows+"/"+first, "")
		rec := request(t, "GET", workflows+"/"+first, "", true, 200)
		if !strings.Contains(rec.Body.String(), first) {
			t.Fatal(rec.Body.String())
		}
		request(t, "GET", base+"/scenarios/"+other+"/workflows/"+first, "", true, 404)
	})
	t.Run("T24 workflow update", func(t *testing.T) {
		p := workflows + "/" + first
		security(t, "PUT", p, body)
		before := request(t, "GET", p, "", true, 200).Body.String()
		exec("CREATE TRIGGER " + schema + ".fixture_admin_update BEFORE INSERT ON " + schema + ".decision_engine_workflow_outcomes FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='rollback'")
		request(t, "PUT", p, strings.Replace(body, "Emit review", "Changed", 1), true, 500)
		exec("DROP TRIGGER " + schema + ".fixture_admin_update")
		if after := request(t, "GET", p, "", true, 200).Body.String(); before != after {
			t.Fatal("update did not roll back")
		}
		rec := request(t, "PUT", p, strings.Replace(body, "Emit review", "Changed", 1), true, 200)
		if !strings.Contains(rec.Body.String(), "Changed") {
			t.Fatal(rec.Body.String())
		}
	})
	second := id(t, request(t, "POST", workflows, strings.Replace(body, "Emit review", "Second", 1), true, 201), "workflow")
	t.Run("T26 workflow reorder", func(t *testing.T) {
		p := workflows + "/reorder"
		b := `["` + second + `","` + first + `"]`
		security(t, "POST", p, b)
		request(t, "POST", p, `[null]`, true, 400)
		request(t, "POST", p, `["`+first+`","`+first+`"]`, true, 422)
		request(t, "POST", p, `[]`, true, 422)
		request(t, "POST", p, b, true, 204)
		rec := request(t, "GET", workflows, "", true, 200)
		var out struct {
			Items []dto.WorkflowResponse `json:"workflows"`
		}
		if e := json.Unmarshal(rec.Body.Bytes(), &out); e != nil || len(out.Items) != 2 || out.Items[0].ID != second || out.Items[1].ID != first || out.Items[1].DisplayOrder != 1 {
			t.Fatal(rec.Body.String(), e)
		}
	})
	t.Run("T25 workflow delete", func(t *testing.T) {
		p := workflows + "/" + first
		security(t, "DELETE", p, "")
		request(t, "DELETE", p, `{"unexpected":true}`, true, 400)
		request(t, "DELETE", base+"/scenarios/"+other+"/workflows/"+first, "", true, 404)
		request(t, "DELETE", p, "", true, 204)
		request(t, "GET", p, "", true, 404)
	})
	live := id(t, request(t, "POST", path+"/iterations", "", true, 201), "iteration")
	phantom := id(t, request(t, "POST", path+"/iterations", "", true, 201), "iteration")
	request(t, "POST", path+"/iterations/"+live+"/rules", `{"name":"true","formula":{"constant":true},"score_modifier":1}`, true, 201)
	request(t, "POST", path+"/iterations/"+live+"/commit", "", true, 200)
	request(t, "POST", path+"/publications", `{"action":"publish","iteration_id":"`+live+`"}`, true, 200)
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	var run string
	t.Run("T27 test run create", func(t *testing.T) {
		p := path + "/test-runs"
		b := `{"phantom_iteration_id":"` + phantom + `","expires_at":"` + expiry + `"}`
		security(t, "POST", p, b)
		request(t, "POST", p, strings.Replace(b, phantom, live, 1), true, 422)
		request(t, "POST", p, strings.Replace(b, expiry, "2000-01-01T00:00:00Z", 1), true, 422)
		request(t, "POST", base+"/scenarios/"+other+"/test-runs", b, true, 422)
		run = id(t, request(t, "POST", p, b, true, 201), "test_run")
		rec := request(t, "GET", base+"/test-runs/"+run, "", true, 200)
		if !strings.Contains(rec.Body.String(), live) || !strings.Contains(rec.Body.String(), `"status":"up"`) {
			t.Fatal(rec.Body.String())
		}
	})
	if run == "" {
		t.Fatal("test run prerequisite failed")
	}
	t.Run("T28 test run cancel", func(t *testing.T) {
		p := base + "/test-runs/" + run + "/cancel"
		security(t, "POST", p, "")
		request(t, "POST", p, `{"actor":"forged"}`, true, 400)
		rec := request(t, "POST", p, "", true, 200)
		if !strings.Contains(rec.Body.String(), `"status":"down"`) {
			t.Fatal(rec.Body.String())
		}
		request(t, "POST", p, "{}", true, 200)
		request(t, "POST", base+"/test-runs/"+uuid.NewString()+"/cancel", "", true, 404)
	})
	t.Run("T29 rule snooze create", func(t *testing.T) {
		p := path + "/rule-snoozes"
		b := `{"object_type":"wallets","object_id":"Wallet ","snooze_group_id":"` + uuid.NewString() + `","expires_at":"` + expiry + `"}`
		security(t, "POST", p, b)
		request(t, "POST", p, strings.Replace(b, expiry, "2000-01-01T00:00:00Z", 1), true, 422)
		created := id(t, request(t, "POST", p, b, true, 201), "rule_snooze")
		rec := request(t, "GET", p+"?object_type=wallets&object_id=Wallet%20", "", true, 200)
		if !strings.Contains(rec.Body.String(), created) {
			t.Fatal(rec.Body.String())
		}
	})
	t.Run("T30 outbox history", func(t *testing.T) {
		p := base + "/outbox-events"
		security(t, "GET", p, "")
		tenant := uuid.MustParse(strings.TrimPrefix(base, "/v1/tenants/"))
		event := uuid.New()
		exec("INSERT INTO "+schema+".decision_engine_outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES(?,?,'fixture','id','event',?,'pending',?)", mysqlstore.UUID(event), mysqlstore.UUID(tenant), mysqlstore.JSON(`{"n":9007199254740993}`), mysqlstore.DateTime{Time: fixtureClock{}.Now()})
		rec := request(t, "GET", p+"?limit=1", "", true, 200)
		if !strings.Contains(rec.Body.String(), event.String()) || !strings.Contains(rec.Body.String(), "9007199254740993") {
			t.Fatal(rec.Body.String())
		}
		for _, q := range []string{"limit=0", "limit=501", "limit=1&limit=2", "cursor=x"} {
			request(t, "GET", p+"?"+q, "", true, 400)
		}
	})
}
