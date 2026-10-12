package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
	"github.com/google/uuid"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestExecutionProjectionRecoveryOrderingAndFences(t *testing.T) {
	db := deliveryDatabase(t)
	ctx := context.Background()
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service"} {
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
	tenant, scenario, iteration, decision, config, execution := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	execSQL(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Projection','projection_test','active',now(),now())`, tenant)
	execSQL(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Projection','customer',now(),now())`, scenario, tenant)
	execSQL(`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$2,$3,1,'live',now())`, iteration, scenario, tenant)
	execSQL(`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$3,$4,'customer-1','customer','review',now())`, decision, tenant, scenario, iteration)
	execSQL(`INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,allowed_outcomes,provider,created_at,updated_at) VALUES($1,$2,$3,'Projection','{review}','test',now(),now())`, config, tenant, scenario)
	source, matches := seedReview(t, db, tenant, 1)
	execSQL(`UPDATE screening.screenings SET decision_id=$2,scenario_id=$3,screening_config_id=$4,idempotency_key=$5 WHERE id=$1`, source, decision, scenario, config, execution)
	projector := store.NewExecutionProjector(db)
	run := func(want int) {
		t.Helper()
		failed, err := projector.RunBatch(ctx, 10)
		if err != nil || failed != want {
			t.Fatalf("projection failures=%d want=%d err=%v", failed, want, err)
		}
	}
	// Missing target is retained with bounded retry instead of losing source work.
	run(1)
	var attempts int
	var last string
	if err := db.QueryRow(ctx, `SELECT attempts,last_error FROM screening.execution_projection_queue WHERE screening_id=$1`, source).Scan(&attempts, &last); err != nil || attempts != 1 || last == "" {
		t.Fatal(attempts, last, err)
	}
	execSQL(`INSERT INTO core.screening_executions(id,tenant_id,config_id,decision_id,scenario_id,status,request_json,created_at) VALUES($1,$2,$3,$4,$5,'pending',jsonb_build_object('idempotency_key',$1::uuid::text),now())`, execution, tenant, config, decision, scenario)
	execSQL(`UPDATE screening.execution_projection_queue SET available_at=now() WHERE screening_id=$1`, source)
	run(0)
	check := func(want string) {
		t.Helper()
		var status, review string
		var actual, version int64
		err := db.QueryRow(ctx, `SELECT e.status,e.response_json->>'status',e.projection_version,s.projection_version FROM core.screening_executions e JOIN screening.screenings s ON s.id=e.projection_source_id AND s.tenant_id=e.tenant_id WHERE e.id=$1`, execution).Scan(&status, &review, &actual, &version)
		if err != nil || review != want || actual != version {
			t.Fatal(status, review, actual, version, err)
		}
	}
	check("awaiting_review")
	// Coalesced writes and a fresh projector simulate worker outage/restart.
	execSQL(`UPDATE screening.screenings SET status='processing',updated_at=now() WHERE id=$1`, source)
	execSQL(`UPDATE screening.screening_matches SET status='confirmed_hit' WHERE id=$1`, matches[0])
	execSQL(`UPDATE screening.screenings SET status='confirmed_hit',updated_at=now() WHERE id=$1`, source)
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.execution_projection_queue`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// A projection error rolls back target changes while persisting retry state.
	execSQL(`CREATE FUNCTION core.reject_projection_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected projection failure'; END $$; CREATE TRIGGER reject_projection_test BEFORE UPDATE ON core.screening_executions FOR EACH ROW EXECUTE FUNCTION core.reject_projection_test()`)
	run(1)
	execSQL(`DROP TRIGGER reject_projection_test ON core.screening_executions; DROP FUNCTION core.reject_projection_test(); UPDATE screening.execution_projection_queue SET available_at=now()`)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.NewExecutionProjector(db).RunBatch(ctx, 10); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check("confirmed_hit")
	// Direct legacy callback reconciliation reads the source, not delayed payload.
	if _, err := db.Exec(ctx, `SELECT core.reconcile_screening_source($1,$2,$3,$4)`, uuid.NewString(), source, decision, config); err == nil {
		t.Fatal("cross tenant projection")
	}
	if _, err := db.Exec(ctx, `SELECT core.reconcile_screening_source($1,$2,$3,$4)`, tenant, source, uuid.NewString(), config); err == nil {
		t.Fatal("wrong decision projection")
	}
	execSQL(`SELECT core.reconcile_screening_source($1,$2,$3,$4)`, tenant, source, decision, config)
	check("confirmed_hit")
	// Source-first lock ordering lets drainers skip an active review without deadlock.
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.screenings SET status='no_hit',updated_at=now() WHERE id=$1`, source); err != nil {
		t.Fatal(err)
	}
	run(0)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	run(0)
	check("no_hit")
	// Crash/failure between target update and acknowledgement rolls both back.
	execSQL(`UPDATE screening.screenings SET status='confirmed_hit',updated_at=now() WHERE id=$1`, source)
	execSQL(`CREATE FUNCTION screening.reject_ack_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected ack failure'; END $$; CREATE TRIGGER reject_ack_test BEFORE DELETE ON screening.execution_projection_queue FOR EACH ROW EXECUTE FUNCTION screening.reject_ack_test()`)
	if _, err = projector.RunBatch(ctx, 10); err == nil {
		t.Fatal("ignored acknowledgement failure")
	}
	if err = db.QueryRow(ctx, `SELECT response_json->>'status' FROM core.screening_executions WHERE id=$1`, execution).Scan(&last); err != nil || last != "no_hit" {
		t.Fatal("partial projection commit", last, err)
	}
	execSQL(`DROP TRIGGER reject_ack_test ON screening.execution_projection_queue; DROP FUNCTION screening.reject_ack_test()`)
	run(0)
	check("confirmed_hit")
	// Real callback HTTP route ignores stale status/provider/count fields.
	if binary := os.Getenv("DECISION_INTEGRATION_BINARY"); binary != "" {
		endpoint, token, _ := startInvestigatorServer(t, db, binary, tenant)
		raw, _ := json.Marshal(map[string]any{"tenant_id": tenant, "screening_id": source, "decision_id": decision, "screening_config_id": config, "status": "failed", "provider_reference": "stale", "match_count": 999})
		req, _ := http.NewRequest(http.MethodPost, endpoint+"/internal/screening-status-updates", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal("callback", resp.StatusCode)
		}
		check("confirmed_hit")
		req, _ = http.NewRequest(http.MethodPost, endpoint+"/v1/tenants/"+tenant+"/screening-executions/"+execution+"/status", bytes.NewBufferString(`{"status":"failed"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal("source-managed status overwrite", resp.StatusCode)
		}
		check("confirmed_hit")
	}
	// Down refuses pending work. Re-upgrade invalidates old version fences and
	// reconciles source changes made while queue triggers were absent.
	migration := func(name string, wantError bool) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", name))
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		_, err = conn.Exec(ctx, string(raw))
		if wantError {
			if err == nil {
				t.Fatal("rollback dropped pending work")
			}
			_, _ = conn.Exec(ctx, "ROLLBACK")
		} else if err != nil {
			t.Fatal(name, err)
		}
	}
	execSQL(`UPDATE screening.screenings SET status='no_hit',updated_at=now() WHERE id=$1`, source)
	migration("000007_execution_projection.down.sql", true)
	run(0)
	check("no_hit")
	migration("000007_execution_projection.down.sql", false)
	execSQL(`UPDATE screening.screenings SET status='confirmed_hit',updated_at=now() WHERE id=$1`, source)
	migration("000007_execution_projection.up.sql", false)
	run(0)
	check("confirmed_hit")
}
