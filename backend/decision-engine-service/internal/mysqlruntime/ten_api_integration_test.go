package mysqlruntime

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func testTenAPIs(t *testing.T, handler http.Handler, base string, db *sql.DB, schema string, revoked *atomic.Bool) {
	request := func(t *testing.T, method, path, body string, auth bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
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
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out[key].ID == "" {
			t.Fatal(rec.Body.String(), err)
		}
		return out[key].ID
	}
	sqlExec := func(t *testing.T, q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	parent := id(t, request(t, "POST", base+"/scenarios", `{"name":"Ten APIs","trigger_object_type":"wallets"}`, true, 201), "scenario")
	other := id(t, request(t, "POST", base+"/scenarios", `{"name":"Other API scope","trigger_object_type":"wallets"}`, true, 201), "scenario")
	path := base + "/scenarios/" + parent
	foreign := base + "/scenarios/" + other
	configs := path + "/scoring-configs"
	body := `{"name":"Scoring","allowed_outcomes":["review","approve","review",""],"ruleset_ref":"rules-v1","config_json":{"threshold":12},"active":true}`
	var config string
	t.Run("T03 scoring create", func(t *testing.T) {
		request(t, "POST", configs, body, false, 401)
		request(t, "POST", configs, `{"name":"bad","allowed_outcomes":[null],"ruleset_ref":"r","config_json":{}}`, true, 400)
		request(t, "POST", configs, `{"name":"bad","allowed_outcomes":[],"ruleset_ref":"r","config_json":{}}`, true, 422)
		request(t, "POST", configs, `{"name":"bad","allowed_outcomes":["review"],"ruleset_ref":"r"}`, true, 422)
		request(t, "POST", configs, strings.TrimSuffix(body, "}")+`,"tenant_id":"forged"}`, true, 400)
		sqlExec(t, "CREATE TRIGGER "+schema+".fixture_scoring_insert BEFORE INSERT ON "+schema+".decision_engine_scoring_outcomes FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private scoring failure'")
		failed := request(t, "POST", configs, body, true, 500)
		if strings.Contains(failed.Body.String(), "private scoring") {
			t.Fatal("SQL error exposed")
		}
		sqlExec(t, "DROP TRIGGER "+schema+".fixture_scoring_insert")
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_scoring_configs WHERE scenario_id=?", mysqlstore.UUID(uuid.MustParse(parent))).Scan(&count); err != nil || count != 0 {
			t.Fatal("create did not roll back", count, err)
		}
		config = id(t, request(t, "POST", configs, body, true, 201), "scoring_config")
	})
	if config == "" {
		t.Fatal("create prerequisite failed")
	}
	t.Run("T01 scoring list", func(t *testing.T) {
		var out struct {
			Configs []dto.ScoringConfigResponse `json:"scoring_configs"`
		}
		rec := request(t, "GET", configs, "", true, 200)
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Configs) != 1 || !reflect.DeepEqual(out.Configs[0].AllowedOutcomes, []string{"review", "approve", "review", ""}) {
			t.Fatal(rec.Body.String(), err)
		}
		request(t, "GET", configs+"?ignored=1", "", true, 400)
		request(t, "GET", base+"/scenarios/"+uuid.NewString()+"/scoring-configs", "", true, 404)
		revoked.Store(true)
		request(t, "GET", configs, "", true, 403)
		revoked.Store(false)
	})
	t.Run("T02 scoring detail", func(t *testing.T) {
		request(t, "GET", configs+"/"+config, "", true, 200)
		request(t, "GET", foreign+"/scoring-configs/"+config, "", true, 404)
		request(t, "GET", strings.Replace(configs, base, "/v1/tenants/"+uuid.NewString(), 1)+"/"+config, "", true, 403)
		request(t, "GET", configs+"/not-a-uuid", "", true, 400)
		request(t, "GET", configs+"/"+uuid.NewString(), "", true, 404)
	})
	updated := `{"name":"Updated scoring","allowed_outcomes":["decline","review"],"ruleset_ref":"rules-v2","config_json":{"threshold":20},"active":false}`
	t.Run("T04 scoring update", func(t *testing.T) {
		before := request(t, "GET", configs+"/"+config, "", true, 200).Body.String()
		sqlExec(t, "CREATE TRIGGER "+schema+".fixture_scoring_update BEFORE INSERT ON "+schema+".decision_engine_scoring_outcomes FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private scoring replacement'")
		request(t, "PUT", configs+"/"+config, updated, true, 500)
		sqlExec(t, "DROP TRIGGER "+schema+".fixture_scoring_update")
		if after := request(t, "GET", configs+"/"+config, "", true, 200).Body.String(); before != after {
			t.Fatal("failed replacement changed configuration", before, after)
		}
		request(t, "PUT", foreign+"/scoring-configs/"+config, updated, true, 404)
		revoked.Store(true)
		request(t, "PUT", configs+"/"+config, updated, true, 403)
		revoked.Store(false)
		var out struct {
			Config dto.ScoringConfigResponse `json:"scoring_config"`
		}
		rec := request(t, "PUT", configs+"/"+config, updated, true, 200)
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Config.ID != config || out.Config.ScenarioID != parent || out.Config.Active || !reflect.DeepEqual(out.Config.AllowedOutcomes, []string{"decline", "review"}) {
			t.Fatal(rec.Body.String(), err)
		}
	})
	t.Run("T05 scoring delete", func(t *testing.T) {
		request(t, "DELETE", foreign+"/scoring-configs/"+config, "", true, 404)
		revoked.Store(true)
		request(t, "DELETE", configs+"/"+config, "", true, 403)
		revoked.Store(false)
		// A disposable restrictive dependent verifies conflict mapping and that
		// failed deletion preserves both the parent and ordered children.
		sqlExec(t, "CREATE TABLE "+schema+".fixture_scoring_reference(config_id BINARY(16) PRIMARY KEY,FOREIGN KEY(config_id) REFERENCES "+schema+".decision_engine_scoring_configs(id))")
		sqlExec(t, "INSERT INTO "+schema+".fixture_scoring_reference VALUES(?)", mysqlstore.UUID(uuid.MustParse(config)))
		request(t, "DELETE", configs+"/"+config, "", true, 409)
		request(t, "GET", configs+"/"+config, "", true, 200)
		sqlExec(t, "DROP TABLE "+schema+".fixture_scoring_reference")
		request(t, "DELETE", configs+"/"+config, `{"unexpected":true}`, true, 400)
		request(t, "DELETE", configs+"/"+config, "", true, 204)
		request(t, "GET", configs+"/"+config, "", true, 404)
		request(t, "DELETE", configs+"/"+config, "", true, 404)
	})
	iteration := id(t, request(t, "POST", path+"/iterations", "", true, 201), "iteration")
	ip := path + "/iterations/" + iteration
	ruleBody := `{"name":"Old rule","formula":{"constant":true},"score_modifier":12}`
	rule := id(t, request(t, "POST", ip+"/rules", ruleBody, true, 201), "rule")
	t.Run("T06 iteration validation", func(t *testing.T) {
		before := request(t, "GET", ip, "", true, 200).Body.String()
		var out struct {
			Validation dto.IterationValidationResponse `json:"validation"`
		}
		rec := request(t, "POST", ip+"/validate", "", true, 200)
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.Validation.Valid || out.Validation.ModelRevision == "" || len(out.Validation.RuleResults) != 1 || !out.Validation.RuleResults[0].Valid {
			t.Fatal(rec.Body.String(), err)
		}
		if after := request(t, "GET", ip, "", true, 200).Body.String(); before != after {
			t.Fatal("validation changed iteration")
		}
		request(t, "PUT", ip+"/rules/"+rule, `{"name":"Bad rule","formula":{"constant":3},"score_modifier":12}`, true, 200)
		rec = request(t, "POST", ip+"/validate", "{}", true, 422)
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Validation.Valid || len(out.Validation.RuleResults[0].Errors) == 0 {
			t.Fatal(rec.Body.String(), err)
		}
		request(t, "POST", foreign+"/iterations/"+iteration+"/validate", "", true, 404)
		request(t, "POST", ip+"/validate", `{"actor":"forged"}`, true, 400)
		revoked.Store(true)
		request(t, "POST", ip+"/validate", "", true, 403)
		revoked.Store(false)
		request(t, "PUT", ip+"/rules/"+rule, ruleBody, true, 200)
	})
	request(t, "POST", ip+"/commit", "", true, 200)
	pub := path + "/publications"
	publish := `{"action":"publish","iteration_id":"` + iteration + `"}`
	request(t, "POST", pub, publish, true, 200)
	t.Run("T07 latest rules", func(t *testing.T) {
		clone := id(t, request(t, "POST", ip+"/draft", "", true, 201), "iteration")
		assertRule := func(iterationID string) {
			t.Helper()
			var out struct {
				Rules []dto.RuleResponse `json:"rules"`
			}
			rec := request(t, "GET", path+"/rules/latest", "", true, 200)
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Rules) != 1 || out.Rules[0].IterationID != iterationID {
				t.Fatal(rec.Body.String(), err)
			}
		}
		assertRule(iteration)
		request(t, "POST", pub, `{"action":"unpublish","iteration_id":"`+iteration+`"}`, true, 200)
		assertRule(clone)
		request(t, "POST", pub, publish, true, 200)
		empty := request(t, "GET", foreign+"/rules/latest", "", true, 200)
		if empty.Body.String() != `{"rules":[]}` {
			t.Fatal(empty.Body.String())
		}
		request(t, "GET", base+"/scenarios/"+uuid.NewString()+"/rules/latest", "", true, 404)
	})
	t.Run("T08 deactivate", func(t *testing.T) {
		request(t, "POST", ip+"/deactivate", `{"iteration_id":"forged"}`, true, 400)
		revoked.Store(true)
		request(t, "POST", ip+"/deactivate", "", true, 403)
		revoked.Store(false)
		sqlExec(t, "CREATE TRIGGER "+schema+".fixture_deactivate_failure BEFORE INSERT ON "+schema+".decision_engine_scenario_publications FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='private deactivate failure'")
		request(t, "POST", ip+"/deactivate", "", true, 500)
		sqlExec(t, "DROP TRIGGER "+schema+".fixture_deactivate_failure")
		sqlExec(t, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed'")
		request(t, "POST", ip+"/deactivate", "{}", true, 200)
		sqlExec(t, "UPDATE "+schema+".data_model_physical_tables SET write_state='open'")
		request(t, "POST", ip+"/deactivate", "", true, 409)
		request(t, "POST", foreign+"/iterations/"+iteration+"/deactivate", "", true, 409)
	})
	t.Run("T09 publication pagination", func(t *testing.T) {
		// Append fixture history with tied timestamps; never rewrite immutable
		// production history or disable its integrity triggers for the test.
		for i := 0; i < 3; i++ {
			sqlExec(t, "INSERT INTO "+schema+".decision_engine_scenario_publications(id,tenant_id,scenario_id,iteration_id,action,created_at) VALUES(?,?,?,?,'publish','2020-01-01 00:00:00.000000')", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.MustParse(strings.TrimPrefix(base, "/v1/tenants/"))), mysqlstore.UUID(uuid.MustParse(parent)), mysqlstore.UUID(uuid.MustParse(iteration)))
		}
		type page struct {
			Publications []dto.PublicationResponse `json:"publications"`
			Next         string                    `json:"next_cursor"`
		}
		read := func(path string) page {
			t.Helper()
			var out page
			rec := request(t, "GET", path, "", true, 200)
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		all := read(pub)
		got := read(pub + "?limit=1")
		ids := []string{got.Publications[0].ID}
		if got.Next == "" {
			t.Fatal("missing cursor")
		}
		request(t, "GET", foreign+"/publications?limit=1&cursor="+url.QueryEscape(got.Next), "", true, 400)
		revoked.Store(true)
		request(t, "GET", pub+"?limit=1&cursor="+url.QueryEscape(got.Next), "", true, 403)
		revoked.Store(false)
		// A later publication must not duplicate or displace the remaining old page.
		request(t, "POST", pub, publish, true, 200)
		for got.Next != "" {
			got = read(pub + "?limit=1&cursor=" + url.QueryEscape(got.Next))
			for _, v := range got.Publications {
				ids = append(ids, v.ID)
			}
			if len(ids) > len(all.Publications) {
				t.Fatal("pagination loop")
			}
		}
		want := []string{}
		for _, v := range all.Publications {
			want = append(want, v.ID)
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatal(ids, want)
		}
		for _, query := range []string{"limit=0", "limit=501", "limit=1&limit=2", "cursor=bad", "limit=1&unknown=true"} {
			request(t, "GET", pub+"?"+query, "", true, 400)
		}
	})
	t.Run("T10 function catalog", func(t *testing.T) {
		request(t, "GET", "/v1/rule-functions", "", false, 401)
		rec := request(t, "GET", "/v1/rule-functions", "", true, 200)
		want, err := json.Marshal(map[string]any{"rule_functions": dto.AdaptRuleFunctionCatalog(ast_eval.SupportedFunctionCatalog())})
		if err != nil || !reflect.DeepEqual(rec.Body.Bytes(), want) {
			t.Fatal("catalog diverged", err)
		}
		request(t, "GET", "/v1/rule-functions?unknown=1", "", true, 400)
	})
}
