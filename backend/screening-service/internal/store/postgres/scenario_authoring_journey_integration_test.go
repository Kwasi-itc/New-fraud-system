package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Uses a real owner API and disposable PostgreSQL; only data-model HTTP is a
// synthetic fixture. Stops at the durable dispatch boundary, not worker delivery.
func TestSavedScenarioScreeningQueuesAuthorizedExecution(t *testing.T) {
	binary := os.Getenv("DECISION_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set DECISION_INTEGRATION_BINARY to a freshly built decision API")
	}
	db := deliveryDatabase(t)
	ctx := context.Background()
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "case-manager-service"} {
		paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", owner, "internal", "migrations", "metadata", "*.up.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatal(owner, err)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			execSQL(string(raw))
		}
	}
	for _, name := range []string{"schema.sql", "schema_v2.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "auth-service", "internal", "platform", name))
		if err != nil {
			t.Fatal(err)
		}
		execSQL(string(raw))
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(db), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	tenant, scenario, iteration, inbox, user, session, table := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	execSQL(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Synthetic','authoring_journey','active',now(),now())`, tenant)
	execSQL(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Synthetic payments','transaction',now(),now())`, scenario, tenant)
	execSQL(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,trigger_formula,created_at) VALUES($1,$2,$3,1,'live','{"constant":true}',now())`, iteration, scenario, tenant)
	execSQL(`UPDATE core.scenarios SET live_iteration_id=$1 WHERE id=$2`, iteration, scenario)
	execSQL(`INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'transaction',now(),now())`, table, tenant)
	execSQL(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,'beneficiary_name','string',now(),now())`, uuid.NewString(), tenant, table)
	execSQL(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Review',now(),now())`, inbox, tenant)
	execSQL(`INSERT INTO identity.users(id,display_name) VALUES($1,'Synthetic administrator')`, user)
	execSQL(`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['platform_admin','case_investigator'])`, tenant, user)
	execSQL(`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,'\x01',$2,$3,now()+interval '1 hour',now()+interval '1 hour')`, session, tenant, user)
	execSQL(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, inbox, user)
	execSQL(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,'other','string',now(),now())`, uuid.NewString(), tenant, table)
	execSQL(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,'birth_text','string',now(),now())`, uuid.NewString(), tenant, table)
	for _, field := range []string{"nationality_text", "country_text", "registration_text"} {
		execSQL(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,$4,'string',now(),now())`, uuid.NewString(), tenant, table, field)
	}
	const serviceToken = "continuous-intake-integration-test-token-32-bytes"
	const authoringToken = "synthetic-authoring-journey-private-token-32-bytes"
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/recognize" {
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				http.Error(w, "invalid", 400)
				return
			}
			if strings.Contains(body["text"], "Recognition Failure") {
				http.Error(w, "synthetic failure", 503)
				return
			}
			matches := []map[string]string{{"type": "Person", "text": body["text"]}}
			if strings.Contains(body["text"], "Recognized Input") {
				matches = append(matches, map[string]string{"type": "Person", "text": "Synthetic Extracted"})
			}
			json.NewEncoder(w).Encode(matches)
			return
		}
		if r.URL.Path == "/internal/screening-capabilities" && r.Header.Get("Authorization") == "Bearer "+testIntakeToken {
			io.WriteString(w, `{"matching_settings_supported":true,"address_supported":true,"birth_date_supported":true,"nationality_supported":true,"country_supported":true,"registration_number_supported":true}`)
			return
		}
		if r.URL.Path != "/v1/tenants/"+tenant+"/data-model" || r.Header.Get("Authorization") != "Bearer "+serviceToken {
			http.Error(w, "unexpected metadata request", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data_model":{"revision_id":"synthetic-1","tables":{"transaction":{"name":"transaction","fields":{"registration_text":{"name":"registration_text","data_type":"string","nullable":true},"nationality_text":{"name":"nationality_text","data_type":"string","nullable":true},"country_text":{"name":"country_text","data_type":"string","nullable":true},"birth_text":{"name":"birth_text","data_type":"string","nullable":true},"beneficiary_name":{"name":"beneficiary_name","data_type":"string","nullable":true},"other":{"name":"other","data_type":"string","nullable":true}},"links_to_single":{}}}}}`)
	}))
	defer model.Close()
	endpoint, token, _ := startInvestigatorServer(t, db, binary, tenant, "SCREENING_AUTHORING_TOKEN="+authoringToken, "DATA_MODEL_SERVICE_URL="+model.URL, "DATA_MODEL_SERVICE_TOKEN="+serviceToken, "SCREENING_SERVICE_URL="+model.URL, "SCREENING_NAME_RECOGNITION_URL="+model.URL+"/recognize")
	client := &http.Client{Timeout: 10 * time.Second}
	request := func(method, path, bearer string, body any, want int) map[string]any {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, endpoint+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Screening-User", user)
		req.Header.Set("X-Screening-Session", session)
		req.Header.Set("X-Screening-Version", "1")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, resp.StatusCode, want, data)
		}
		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	base := "/internal/screening-authoring/tenants/" + tenant + "/scenarios/" + scenario
	save := func(entity string, enabled bool, outcome string) map[string]any {
		t.Helper()
		id := uuid.NewString()
		return request("PUT", base+"/configs/"+id, authoringToken, map[string]any{"id": id, "name": entity + " " + outcome, "enabled": enabled, "entity_type": entity, "name_field": "beneficiary_name", "outcomes": []string{outcome}, "inbox_id": inbox}, 200)
	}
	person := save("Person", true, "approve")
	organization := save("Organization", true, "approve")
	save("Person", false, "approve")
	save("Person", true, "review")
	request("GET", base, token, nil, 401)
	request("GET", "/internal/screening-authoring/tenants/"+uuid.NewString()+"/scenarios/"+scenario, authoringToken, nil, 403)
	evaluate := func(fields map[string]any, want int) map[string]any {
		t.Helper()
		return request("POST", "/v1/tenants/"+tenant+"/scenarios/"+scenario+"/evaluate", token, map[string]any{"object_id": uuid.NewString(), "object_type": "transaction", "fields": fields}, want)
	}
	out := evaluate(map[string]any{"beneficiary_name": "  Synthetic Example  "}, 200)
	result := out["result"].(map[string]any)
	decision := result["decision"].(map[string]any)
	if decision["outcome"] != "approve" {
		t.Fatal("unexpected machine outcome", decision["outcome"])
	}
	check := func(decisionID string, expected map[string]string) {
		t.Helper()
		rows, err := db.Query(ctx, `SELECT id::text,config_id::text,tenant_id::text,scenario_id::text,status,request_json FROM core.screening_executions WHERE decision_id=$1`, decisionID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id, config, tid, sid, status string
			var raw []byte
			if err := rows.Scan(&id, &config, &tid, &sid, &status, &raw); err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			entity, ok := expected[config]
			if !ok || tid != tenant || sid != scenario || status != "pending" || payload["decision_id"] != decisionID || payload["idempotency_key"] != id {
				t.Fatal("invalid execution linkage", config, status, payload)
			}
			queries := payload["queries"].([]any)
			query := queries[0].(map[string]any)
			if query["name"] != "Synthetic Example" || query["type"] != entity {
				t.Fatal("incorrect name mapping", query)
			}
			var queued int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind='screening_execution' AND args->>'execution_id'=$1 AND args->>'tenant_id'=$2`, id, tenant).Scan(&queued); err != nil {
				t.Fatal(err)
			}
			if queued != 1 {
				t.Fatal("execution must have exactly one durable job", queued)
			}
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if count != len(expected) {
			t.Fatal("wrong execution count", count, len(expected))
		}
	}
	check(decision["id"].(string), map[string]string{person["id"].(string): "Person", organization["id"].(string): "Organization"})
	// Saving through the same API must invalidate its active-config cache.
	person["enabled"] = false
	request("PUT", base+"/configs/"+person["id"].(string), authoringToken, person, 200)
	out = evaluate(map[string]any{"beneficiary_name": "Synthetic Example"}, 200)
	check(out["result"].(map[string]any)["decision"].(map[string]any)["id"].(string), map[string]string{organization["id"].(string): "Organization"})
	var before, after int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	evaluate(map[string]any{"other": "no mapped name"}, 422)
	evaluate(map[string]any{"beneficiary_name": "   "}, 422)
	if err := db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("failed mapping persisted a decision", before, after)
	}
	// A queue insertion failure must roll back the decision, execution and
	// outbox together. This trigger exists only in this disposable database.
	counts := func() []int {
		t.Helper()
		var decisions, executions, events, jobs int
		err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.decisions),
			(SELECT count(*) FROM core.screening_executions),
			(SELECT count(*) FROM core.outbox_events),
			(SELECT count(*) FROM river_job)`).Scan(&decisions, &executions, &events, &jobs)
		if err != nil {
			t.Fatal(err)
		}
		return []int{decisions, executions, events, jobs}
	}
	baseline := counts()
	execSQL(`CREATE FUNCTION public.reject_journey_queue() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.kind='screening_execution' THEN RAISE EXCEPTION 'synthetic queue failure'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER reject_journey_queue BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION public.reject_journey_queue()`)
	evaluate(map[string]any{"beneficiary_name": "Synthetic Example"}, 500)
	for i, got := range counts() {
		if got != baseline[i] {
			t.Fatal("queue failure left partial persistence", baseline, counts())
		}
	}
	execSQL(`DROP TRIGGER reject_journey_queue ON river_job`)
	execSQL(`UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, session)
	request("GET", base, authoringToken, nil, 403)
	t.Run("local worker provider and case delivery", func(t *testing.T) {
		runAuthoredScreeningWorkers(t, db, tenant, inbox, endpoint, token, authoringToken, func(name string) string {
			out := evaluate(map[string]any{"beneficiary_name": name}, 200)
			return out["result"].(map[string]any)["decision"].(map[string]any)["id"].(string)
		})
	})
	ignoreList := uuid.NewString()
	execSQL(`INSERT INTO core.custom_lists(id,tenant_id,name,description,kind,created_at,updated_at) VALUES($1,$2,'Suffixes','','generic_text',now(),now())`, ignoreList, tenant)
	execSQL(`INSERT INTO core.custom_list_entries(id,tenant_id,list_id,list_name,value,created_at) VALUES($1,$2,$3,'Suffixes','ltd',now())`, uuid.NewString(), tenant, ignoreList)
	// Address dispatch proof is separate from the name-only worker journey above.
	execSQL(`UPDATE identity.sessions SET revoked_at=NULL WHERE id=$1`, session)
	id := uuid.NewString()
	request("PUT", base+"/configs/"+id, authoringToken, map[string]any{"id": id, "name": "Address mapping", "nationality_field": "nationality_text", "use_ner": true, "ignore_list_id": ignoreList, "minimum_name_length": 5, "remove_digits": true, "condition": map[string]any{"kind": "payload", "field": "beneficiary_name", "operator": "contains", "value": "Synthetic"}, "threshold": 85, "result_limit": 7, "datasets": []string{"synthetic", "other-test"}, "birth_date_field": "birth_text", "enabled": true, "entity_type": "Person", "name_field": "beneficiary_name", "address_field": "other", "outcomes": []string{"approve"}, "inbox_id": inbox}, 200)
	addressResult := evaluate(map[string]any{"beneficiary_name": "Synthetic Address", "other": "  Synthetic Street  "}, 200)
	decisionID := addressResult["result"].(map[string]any)["decision"].(map[string]any)["id"].(string)
	var address string
	if err := db.QueryRow(ctx, `SELECT request_json->'queries'->0->>'address' FROM core.screening_executions WHERE decision_id=$1 AND config_id=$2`, decisionID, id).Scan(&address); err != nil || address != "Synthetic Street" {
		t.Fatal("saved address did not reach dispatch", address, err)
	}
	evaluate(map[string]any{"beneficiary_name": "Synthetic Address", "other": 42}, 422)
	baselineBirth := counts()
	for _, value := range []any{"2001-02-29", "2999", 1990} {
		evaluate(map[string]any{"beneficiary_name": "Synthetic Invalid Birth", "birth_text": value}, 422)
	}
	for i, got := range counts() {
		if got != baselineBirth[i] {
			t.Fatal("invalid birth date left writes", baselineBirth, counts())
		}
	}

	// Retire the remaining name-only rule before the address-specific journeys.
	organization["enabled"] = false
	request("PUT", base+"/configs/"+organization["id"].(string), authoringToken, organization, 200)
	orgAddressID := uuid.NewString()
	request("PUT", base+"/configs/"+orgAddressID, authoringToken, map[string]any{"id": orgAddressID, "use_ner": true, "ignore_list_id": ignoreList, "minimum_name_length": 5, "remove_digits": true, "condition": map[string]any{"kind": "payload", "field": "beneficiary_name", "operator": "contains", "value": "Synthetic"}, "threshold": 85, "result_limit": 7, "datasets": []string{"synthetic", "other-test"}, "name": "Organization address", "registration_number_field": "registration_text", "country_field": "country_text", "enabled": true, "entity_type": "Organization", "name_field": "beneficiary_name", "address_field": "other", "outcomes": []string{"approve"}, "inbox_id": inbox}, 200)
	notTriggered := evaluate(map[string]any{"beneficiary_name": "Outside policy"}, 200)
	unmatchedID := notTriggered["result"].(map[string]any)["decision"].(map[string]any)["id"].(string)
	var unmatched int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM core.screening_executions WHERE decision_id=$1`, unmatchedID).Scan(&unmatched); err != nil || unmatched != 0 {
		t.Fatal("false screening condition queued work", unmatched, err)
	}
	skippedConfig := uuid.NewString()
	request("PUT", base+"/configs/"+skippedConfig, authoringToken, map[string]any{"id": skippedConfig, "name": "Skip short names", "entity_type": "Person", "name_field": "beneficiary_name", "enabled": true, "outcomes": []string{"approve"}, "inbox_id": inbox, "minimum_name_length": 200}, 200)
	skippedResult := evaluate(map[string]any{"beneficiary_name": "Outside policy"}, 200)
	skippedDecision := skippedResult["result"].(map[string]any)["decision"].(map[string]any)["id"].(string)
	if err := db.QueryRow(ctx, `SELECT count(*) FROM core.screening_executions WHERE decision_id=$1`, skippedDecision).Scan(&unmatched); err != nil || unmatched != 0 {
		t.Fatal("preprocessing skip queued work", unmatched, err)
	}
	invalidSettingsBaseline := counts()
	for _, bad := range []map[string]any{{"threshold": -1}, {"threshold": 101}, {"threshold": 1.5}, {"result_limit": 0}, {"result_limit": 51}, {"datasets": []string{"synthetic", "synthetic"}}, {"datasets": []string{"../bad"}}, {"minimum_name_length": -1}, {"condition": map[string]any{"kind": "all"}}} {
		bad["id"] = uuid.NewString()
		bad["name"] = "Invalid settings"
		bad["entity_type"] = "Person"
		bad["name_field"] = "beneficiary_name"
		bad["outcomes"] = []string{"approve"}
		bad["inbox_id"] = inbox
		request("PUT", base+"/configs/"+bad["id"].(string), authoringToken, bad, 400)
	}
	for i, got := range counts() {
		if got != invalidSettingsBaseline[i] {
			t.Fatal("invalid settings persisted writes", invalidSettingsBaseline, counts())
		}
	}
	recognitionBaseline := counts()
	evaluate(map[string]any{"beneficiary_name": "Synthetic Recognition Failure"}, 502)
	for i, got := range counts() {
		if got != recognitionBaseline[i] {
			t.Fatal("failed recognition left writes", recognitionBaseline, counts())
		}
	}
	countryBaseline := counts()
	for _, field := range []string{"nationality_text", "country_text"} {
		for _, value := range []any{"ZZ", "001", 42} {
			evaluate(map[string]any{"beneficiary_name": "Synthetic Invalid Country", field: value}, 422)
		}
	}
	for i, got := range counts() {
		if got != countryBaseline[i] {
			t.Fatal("invalid country left writes", countryBaseline, counts())
		}
	}

	registrationBaseline := counts()
	evaluate(map[string]any{"beneficiary_name": "Synthetic Invalid Address", "other": strings.Repeat("x", 301)}, 422)
	for _, value := range []any{42, strings.Repeat("x", 101)} {
		evaluate(map[string]any{"beneficiary_name": "Synthetic Invalid Registration", "registration_text": value}, 422)
	}
	for i, got := range counts() {
		if got != registrationBaseline[i] {
			t.Fatal("invalid registration left writes", registrationBaseline, counts())
		}
	}

	for _, direct := range []bool{true, false} {
		name := "address direct worker journey"
		if !direct {
			name = "address worker configuration drift"
		}
		t.Run(name, func(t *testing.T) {
			runAddressScreeningWorkers(t, db, tenant, inbox, endpoint, token, direct, map[string]string{id: "Person", orgAddressID: "Organization"}, func(fields map[string]any) string {
				out := evaluate(fields, 200)
				return out["result"].(map[string]any)["decision"].(map[string]any)["id"].(string)
			})
		})
	}

	missingListBaseline := counts()
	execSQL(`DELETE FROM core.custom_list_entries WHERE tenant_id=$1 AND list_id=$2`, tenant, ignoreList)
	execSQL(`DELETE FROM core.custom_lists WHERE tenant_id=$1 AND id=$2`, tenant, ignoreList)
	evaluate(map[string]any{"beneficiary_name": "Synthetic Missing List"}, 422)
	for i, got := range counts() {
		if got != missingListBaseline[i] {
			t.Fatal("missing list left decision writes", missingListBaseline, counts())
		}
	}

}
