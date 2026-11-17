package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func TestInlineDecisionRealBinaryOrderingFailureRollbackAndHistory(t *testing.T) {
	binary := os.Getenv("DECISION_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("fresh decision binary required")
	}
	f := newInternalFixture(t)
	db := f.db
	ctx := context.Background()
	tenant := f.a.Tenant().String()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	scenario, iteration, inbox, config := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Inline end-to-end','transaction',now(),now())`, scenario, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,trigger_formula,created_at) VALUES($1,$2,$3,1,'live','{"constant":true}',now())`, iteration, scenario, tenant)
	exec(`UPDATE core.scenarios SET live_iteration_id=$1 WHERE id=$2`, iteration, scenario)
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Inline review',now(),now())`, inbox, tenant)
	selection := sourcedata.Selection{Version: 2, Profile: sourcedata.Profile, Threshold: 100, Datasets: []sourcedata.DatasetRef{{Scope: "tenant", ID: f.private.Dataset}}}
	cfg, _ := json.Marshal(map[string]any{"authoring_version": 2, "entity_type": "Person", "query_fields": map[string]string{"name": "beneficiary_name"}, "provider_config": selection, "forced_outcome": "block_and_review"})
	exec(`INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,allowed_outcomes,provider,config_json,active,review_inbox_id,created_at,updated_at) VALUES($1,$2,$3,'Inline check','{approve}','internal-watchlists',$4,true,$5,now(),now())`, config, tenant, scenario, cfg, inbox)
	exec(`INSERT INTO core.scoring_configs(id,tenant_id,scenario_id,name,allowed_outcomes,ruleset_ref,created_at,updated_at) VALUES($1,$2,$3,'Only final blocked decisions','{block_and_review}','fixture-no-dispatch',now(),now())`, uuid.NewString(), tenant, scenario)
	migrator, e := rivermigrate.New(riverpgxv5.New(db), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); e != nil {
		t.Fatal(e)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data_model": map[string]any{"revision_id": "inline-fixture-v1", "tables": map[string]any{"transaction": map[string]any{"name": "transaction", "fields": map[string]any{"beneficiary_name": map[string]any{"name": "beneficiary_name", "data_type": "string", "nullable": false}}, "links_to_single": map[string]any{}}}}})
	}))
	defer model.Close()
	start := func(failure string) (string, string) {
		url, token, _ := startInvestigatorServer(t, db, binary, tenant, "DATA_MODEL_SERVICE_URL="+model.URL, "INTERNAL_SCREENING_INLINE_ENABLED=true", "INTERNAL_SCREENING_FAILURE_POLICY="+failure, "INTERNAL_SCREENING_TIMEOUT=1s")
		return url, token
	}
	url, token := start("error")
	client := &http.Client{Timeout: 5 * time.Second}
	evaluate := func(base, name string, wantSuccess bool) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"object_id": uuid.NewString(), "object_type": "transaction", "fields": map[string]any{"beneficiary_name": name}})
		req, _ := http.NewRequest("POST", base+"/v1/tenants/"+tenant+"/scenarios/"+scenario+"/evaluate", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if (response.StatusCode == 200) != wantSuccess {
			t.Fatalf("evaluation status %d: %s", response.StatusCode, raw)
		}
		if !wantSuccess {
			return ""
		}
		var out struct {
			Result struct {
				Decision struct {
					ID string `json:"id"`
				}
			}
		}
		if json.Unmarshal(raw, &out) != nil || out.Result.Decision.ID == "" {
			t.Fatal(string(raw))
		}
		return out.Result.Decision.ID
	}
	var fixture struct {
		Version int                 `json:"version"`
		Source  string              `json:"source"`
		Records []sourcedata.Record `json:"records"`
	}
	json.Unmarshal(f.input, &fixture)
	person := ""
	for _, record := range fixture.Records {
		if record.Type == "Person" {
			person = record.Name
			break
		}
	}
	hit := evaluate(url, person, true)
	var outcome string
	var score, scorings, evidence int
	if e = db.QueryRow(ctx, `SELECT outcome,score,(SELECT count(*) FROM core.scoring_requests WHERE decision_id=$1),(SELECT count(*) FROM screening.inline_evidence WHERE decision_id=$1) FROM core.decisions WHERE id=$1`, hit).Scan(&outcome, &score, &scorings, &evidence); e != nil || outcome != "block_and_review" || score != 0 || scorings != 1 || evidence != 1 {
		t.Fatal("final outcome before scoring", outcome, score, scorings, evidence, e)
	}
	var pending int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM core.screening_executions WHERE decision_id=$1 AND status='pending'`, hit).Scan(&pending); e != nil || pending != 0 {
		t.Fatal("synchronous lookup queued again", pending, e)
	}
	clean := evaluate(url, "No Fictional Catalog Candidate", true)
	if e = db.QueryRow(ctx, `SELECT outcome,(SELECT count(*) FROM core.scoring_requests WHERE decision_id=$1) FROM core.decisions WHERE id=$1`, clean).Scan(&outcome, &scorings); e != nil || outcome != "approve" || scorings != 0 {
		t.Fatal("clean decision", outcome, scorings, e)
	}
	// A batch includes a plain scenario that would otherwise commit before a
	// second scenario's failed lookup. Both successful and rollback paths use
	// the actual HTTP handler and freshly built decision service.
	other, otherIteration := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Batch companion','transaction',now(),now())`, other, tenant)
	exec(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,trigger_formula,created_at) VALUES($1,$2,$3,1,'live','{"constant":true}',now())`, otherIteration, other, tenant)
	exec(`UPDATE core.scenarios SET live_iteration_id=$1 WHERE id=$2`, otherIteration, other)
	batch := func(name string, success bool) {
		t.Helper()
		var before, after int
		db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&before)
		body, _ := json.Marshal(map[string]any{"object_id": uuid.NewString(), "object_type": "transaction", "fields": map[string]any{"beneficiary_name": name}})
		r, _ := http.NewRequest("POST", url+"/v1/tenants/"+tenant+"/decisions/all", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if (response.StatusCode == 200) != success {
			t.Fatalf("batch status %d: %s", response.StatusCode, raw)
		}
		db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&after)
		want := before
		if success {
			want += 2
		}
		if after != want {
			t.Fatalf("batch partially committed: before=%d after=%d want=%d", before, after, want)
		}
	}
	batch(person, true)
	var before int
	db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&before)
	exec(`CREATE FUNCTION screening.fail_inline_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected evidence write failure'; END $$; CREATE TRIGGER fail_inline_test BEFORE INSERT ON screening.inline_evidence FOR EACH ROW EXECUTE FUNCTION screening.fail_inline_test()`)
	evaluate(url, person, false)
	batch(person, false)
	var after int
	db.QueryRow(ctx, `SELECT count(*) FROM core.decisions`).Scan(&after)
	if after != before {
		t.Fatal("decision escaped failed evidence transaction")
	}
	exec(`DROP TRIGGER fail_inline_test ON screening.inline_evidence; DROP FUNCTION screening.fail_inline_test()`)
	// More exact candidates than the configured output bound is incomplete, never clean.
	many := fixture
	many.Records = nil
	for i := 0; i < 60; i++ {
		many.Records = append(many.Records, sourcedata.Record{ID: fmt.Sprintf("many-%03d", i), Name: "Many Candidate", Type: "Person", SourceReference: "Synthetic overflow"})
	}
	input, _ := json.Marshal(many)
	ingest := sourceingest.Store{DB: db}
	gen, e := ingest.Stage(ctx, f.private, bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, f.private, gen); e != nil {
		t.Fatal(e)
	}
	evaluate(url, "Many Candidate", false)
	batch("Many Candidate", false)
	var failedCount int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM screening.inline_evidence WHERE lookup_status IN ('incomplete','failed','timed_out')`).Scan(&failedCount); e != nil || failedCount != 0 {
		t.Fatal("failed lookup saved a successful decision", failedCount, e)
	}
	var old []byte
	if e = db.QueryRow(ctx, `SELECT provider_evidence FROM screening.inline_evidence WHERE decision_id=$1`, hit).Scan(&old); e != nil || !bytes.Contains(old, []byte(f.version.String())) {
		t.Fatal("historical version changed", e)
	}
	exec(`DELETE FROM screening.source_grants WHERE tenant_id=$1 AND owner_scope=$1 AND dataset_id=$2`, tenant, f.private.Dataset)
	evaluate(url, person, false)
}
