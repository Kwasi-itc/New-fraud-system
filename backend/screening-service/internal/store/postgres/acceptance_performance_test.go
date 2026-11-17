package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
)

func TestAcceptanceCompleteDecisionWorkload(t *testing.T) {
	if os.Getenv("SCREENING_RUN_ACCEPTANCE_LOAD") != "true" {
		t.Skip("explicit opt-in for full decision/worker acceptance workload")
	}
	count, requests, concurrency := 40000, 1000, 16
	if raw := os.Getenv("SCREENING_ACCEPTANCE_REQUESTS"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 20 || v > 10000 {
			t.Fatal("invalid request count")
		}
		requests = v
	}
	if raw := os.Getenv("SCREENING_ACCEPTANCE_CONCURRENCY"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 64 {
			t.Fatal("invalid concurrency")
		}
		concurrency = v
	}
	h := newAcceptanceHarness(t, count)
	ctx := context.Background()
	db := h.f.db
	tenant := h.f.a.Tenant().String()
	endpoint := h.decisionURL + "/v1/tenants/" + tenant + "/scenarios/" + h.scenario + "/evaluate"
	body := func(i int, name string) map[string]any {
		amount := 500
		if i%2 == 0 {
			amount = 25000
		}
		country := "GH"
		if i%10 == 0 {
			country = "IR"
		}
		return map[string]any{"object_id": fmt.Sprintf("acceptance-%06d-%s", i, uuid.NewString()), "object_type": "transaction", "fields": map[string]any{"beneficiary_name": name, "amount": amount, "country": country}}
	}
	if status, _, e := h.request("POST", endpoint, "wrong", body(-1, "Unauthorized")); e != nil || status != 401 {
		t.Fatal("authentication probe", status, e)
	}
	foreign := h.decisionURL + "/v1/tenants/" + h.f.b.Tenant().String() + "/scenarios/" + h.scenario + "/evaluate"
	if status, _, e := h.request("POST", foreign, h.token, body(-1, "Foreign scenario")); e != nil || status < 400 {
		t.Fatal("tenant/scenario consistency probe", status, e)
	}
	// Prepare genuine signed-human manual searches and subscriptions. The real
	// screening worker, not a queue receipt, completes these and their rescreens.
	const monitors = 20
	for i := 0; i < monitors; i++ {
		input := map[string]any{"operation_id": uuid.NewString(), "provider": sourcedata.Provider, "profile": sourcedata.Profile, "dataset_selection": []sourcedata.DatasetRef{{Scope: "tenant", ID: h.f.private.Dataset}}, "name": fmt.Sprintf("Future Acceptance Designation %04d", i), "entity_type": "Person", "threshold": 100, "limit": 10}
		status, raw, e := h.request("POST", h.screeningURL+"/v1/manual-screenings", h.human, input)
		if e != nil || status != 202 {
			t.Fatalf("manual setup status=%d error=%v body=%s", status, e, raw)
		}
		var result struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &result) != nil || result.ID == "" {
			t.Fatal("manual ID")
		}
		waitFor(t, func() bool {
			var n int
			return db.QueryRow(ctx, `SELECT count(*) FROM screening.screenings WHERE id=$1 AND completed_at IS NOT NULL`, result.ID).Scan(&n) == nil && n == 1
		})
		if status, raw, e = h.request("POST", h.screeningURL+"/v1/manual-screenings/"+result.ID+"/monitor", h.human, map[string]bool{"enabled": true}); e != nil || status != 200 {
			t.Fatalf("monitor setup %d %v %s", status, e, raw)
		}
	}
	waitFor(t, func() bool {
		var n int
		return db.QueryRow(ctx, `SELECT count(*) FROM screening.source_rescreen_targets WHERE status='completed'`).Scan(&n) == nil && n == monitors
	})
	if os.Getenv("SCREENING_ACCEPTANCE_SQL_PROFILE") == "true" {
		if _, e := db.Exec(ctx, `SELECT pg_stat_statements_reset()`); e != nil {
			t.Fatal(e)
		}
	}
	var errorsCount, incomplete atomic.Int64
	type sample struct {
		latency  time.Duration
		decision string
		hit      bool
	}
	samples := make(chan sample, requests)
	problems := make(chan string, requests)
	jobs := make(chan int, requests)
	for i := 0; i < requests; i++ {
		jobs <- i
	}
	close(jobs)
	// A complete new version, with additions and modifications, overlaps requests.
	publication := make(chan error, 1)
	go func() {
		records := append([]sourcedata.Record(nil), h.records...)
		records[0].SourceReference = "Updated fictional acceptance publication"
		for i := 0; i < monitors; i++ {
			records = append(records, sourcedata.Record{ID: fmt.Sprintf("future-%04d", i), Name: fmt.Sprintf("Future Acceptance Designation %04d", i), Type: "Person", SourceReference: "Fictional newly added designation"})
		}
		raw, marshalErr := acceptanceSnapshot(records)
		if marshalErr != nil {
			publication <- marshalErr
			return
		}
		s := sourceingest.Store{DB: db}
		gen, e := s.Stage(ctx, h.f.private, bytes.NewReader(raw))
		if e == nil {
			_, e = s.Publish(ctx, h.f.private, gen)
		}
		publication <- e
	}()
	type dbObservation struct {
		At                               time.Time `json:"at"`
		Active, Waiting, Queued, Running int
	}
	observations := []dbObservation{}
	var obsMu sync.Mutex
	stop := make(chan struct{})
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var a, w, q, r int
				e := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active'),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),(SELECT count(*) FROM river_job WHERE state IN ('available','scheduled','retryable')),(SELECT count(*) FROM river_job WHERE state='running')`).Scan(&a, &w, &q, &r)
				if e == nil {
					obsMu.Lock()
					observations = append(observations, dbObservation{time.Now().UTC(), a, w, q, r})
					obsMu.Unlock()
				}
			}
		}
	}()
	started := time.Now()
	var wg sync.WaitGroup
	for c := 0; c < concurrency; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				hit := i%2 == 0
				name := "Fictional No Catalog Candidate Qxvjr Unlisted"
				if hit {
					index := (i * 13) % len(h.records)
					for h.records[index].Type != "Person" {
						index = (index + 1) % len(h.records)
					}
					name = h.records[index].Name
					if i%10 == 2 {
						r := []rune(name)
						r[2] = 'z'
						name = string(r)
					}
				}
				begin := time.Now()
				status, raw, e := h.request("POST", endpoint, h.token, body(i, name))
				latency := time.Since(begin)
				if e != nil || status != 200 {
					errorsCount.Add(1)
					if bytes.Contains(raw, []byte("incomplete")) {
						incomplete.Add(1)
					}
					if len(raw) > 1000 {
						raw = raw[:1000]
					}
					problems <- fmt.Sprintf("status=%d transport_error=%t body=%s", status, e != nil, raw)
					continue
				}
				var result struct {
					Result struct {
						Decision struct {
							ID, Outcome string
							Score       int
						}
					}
				}
				if json.Unmarshal(raw, &result) != nil || result.Result.Decision.ID == "" {
					errorsCount.Add(1)
					problems <- "invalid decision response"
					continue
				}
				if hit && result.Result.Decision.Outcome != "block_and_review" {
					errorsCount.Add(1)
					problems <- "expected screening forced outcome missing"
				}
				samples <- sample{latency, result.Result.Decision.ID, hit}
			}
		}()
	}
	wg.Wait()
	apiWall := time.Since(started)
	close(samples)
	close(problems)
	if e := <-publication; e != nil {
		t.Fatal("concurrent publication", e)
	}
	latencies := []time.Duration{}
	ids := []string{}
	hits := 0
	for s := range samples {
		latencies = append(latencies, s.latency)
		ids = append(ids, s.decision)
		if s.hit {
			hits++
		}
	}
	// Count delivery as complete only after persisted status and HTTP receipts.
	deadline := time.Now().Add(90 * time.Second)
	var pendingWorkflow, pendingEvents, rescreens int
	for {
		e := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.workflow_executions WHERE status<>'dispatched'),(SELECT count(*) FROM core.outbox_events WHERE status<>'sent'),(SELECT count(*) FROM screening.source_rescreen_targets t JOIN screening.source_publications p ON p.id=t.publication_id JOIN screening.source_datasets d ON(d.owner_scope,d.id,d.active_generation)=(p.owner_scope,p.dataset_id,p.generation) WHERE t.status='completed')`).Scan(&pendingWorkflow, &pendingEvents, &rescreens)
		if e != nil {
			t.Fatal(e)
		}
		if pendingWorkflow == 0 && pendingEvents == 0 && rescreens == monitors {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	drainedWall := time.Since(started)
	close(stop)
	<-samplerDone
	var decisions, evidence, executions, rules, workflows, events, attempts, failedJobs int
	e := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.decisions WHERE scenario_id=$1),(SELECT count(*) FROM screening.inline_evidence WHERE scenario_id=$1),(SELECT count(*) FROM core.screening_executions WHERE scenario_id=$1 AND status='completed'),(SELECT count(*) FROM core.rule_executions x JOIN core.decisions d ON d.id=x.decision_id WHERE d.scenario_id=$1),(SELECT count(*) FROM core.workflow_executions),(SELECT count(*) FROM core.outbox_events),(SELECT coalesce(sum(greatest(attempt-1,0)),0) FROM river_job),(SELECT count(*) FROM river_job WHERE state IN ('discarded','cancelled'))`, h.scenario).Scan(&decisions, &evidence, &executions, &rules, &workflows, &events, &attempts, &failedJobs)
	if e != nil {
		t.Fatal(e)
	}
	var generations int
	if e = db.QueryRow(ctx, `SELECT count(DISTINCT provider_evidence->'datasets') FROM screening.inline_evidence WHERE scenario_id=$1`, h.scenario).Scan(&generations); e != nil {
		t.Fatal(e)
	}
	var badAttribution int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM screening.inline_evidence e CROSS JOIN LATERAL jsonb_array_elements(e.provider_evidence->'datasets') d WHERE e.scenario_id=$1 AND NOT EXISTS(SELECT 1 FROM screening.source_versions v WHERE v.owner_scope=CASE WHEN d->'ref'->>'scope'='global' THEN '00000000-0000-0000-0000-000000000000'::uuid ELSE e.tenant_id END AND v.dataset_id=d->'ref'->>'id' AND v.id=(d->>'generation')::uuid AND v.status='published')`, h.scenario).Scan(&badAttribution); e != nil || badAttribution != 0 {
		t.Fatal("saved generation attribution", badAttribution, e)
	}
	status, metrics, metricsErr := h.request("GET", h.decisionURL+"/v1/admin/runtime-metrics", h.token, nil)
	var runtimeMetrics any
	if metricsErr == nil && status == 200 {
		json.Unmarshal(metrics, &runtimeMetrics)
	}
	diag, e := (sourcecollector.Collector{DB: db}).Diagnostics(ctx)
	if e != nil {
		t.Fatal(e)
	}
	h.mu.Lock()
	workflowReceipts, eventReceipts := len(h.workflowReceipts), len(h.eventReceipts)
	h.mu.Unlock()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	percentile := func(p int) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return float64(latencies[(len(latencies)-1)*p/100].Microseconds()) / 1000
	}
	report := map[string]any{"dataset_records": count, "requests": requests, "concurrency": concurrency, "request_mix": "50% no candidate, 40% exact-name hit, 10% one-character spelling hit; 2 payload fraud rules; all decisions enqueue a workflow and outbox events", "api_completed_decisions": decisions, "api_throughput_per_second": float64(decisions) / apiWall.Seconds(), "api_wall_ms": apiWall.Milliseconds(), "api_p50_ms": percentile(50), "api_p95_ms": percentile(95), "api_p99_ms": percentile(99), "decision_and_effects_drained_throughput_per_second": float64(decisions) / drainedWall.Seconds(), "decision_and_effects_wall_ms": drainedWall.Milliseconds(), "errors": errorsCount.Load(), "incomplete": incomplete.Load(), "inline_evidence": evidence, "completed_screening_executions": executions, "rule_executions": rules, "expected_screening_hits": hits, "workflow_executions": workflows, "workflow_http_receipts": workflowReceipts, "outbox_events": events, "outbox_http_receipts": eventReceipts, "pending_workflows": pendingWorkflow, "pending_outbox": pendingEvents, "current_generation_rescreens_completed": rescreens, "retries": attempts, "discarded_or_cancelled_jobs": failedJobs, "source_generation_sets": generations, "concurrent_full_publication": true, "source_diagnostics": json.RawMessage(diag), "database_observations_100ms": observations, "runtime_metrics": runtimeMetrics, "authentication": "actual machine service-token authentication for decision API; actual signed/current human sessions for monitoring setup. Not an OIDC token-issuance benchmark.", "environment": "Go/PG Docker Desktop shared host; fresh decision/screening/case binaries and decision/screening workers; PostgreSQL 16; plain local container HTTP; controlled HTTP model-read and outbound receipt dependencies", "limitations": []string{"Fictional mechanical catalog and closed-loop clients, not official-source customer traffic or an EC2/RDS sizing acceptance", "API latency ends after transaction commit; drained throughput also waits for workflow/outbox receipts and concurrent source publication/rescreening", "100ms database sampling and HTTP/SQL observers add overhead; CPU/RSS snapshots are recorded separately", "No source rights or matching-quality acceptance is inferred"}, "production_capacity_accepted": false}
	if os.Getenv("SCREENING_ACCEPTANCE_SQL_PROFILE") == "true" {
		var stats json.RawMessage
		if e = db.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(q)),'[]'::jsonb) FROM(SELECT left(query,1600) AS normalized_query,calls,total_exec_time,mean_exec_time,max_exec_time,rows,shared_blks_hit,shared_blks_read,temp_blks_written,jit_functions,jit_generation_time,jit_inlining_time,jit_optimization_time,jit_emission_time FROM public.pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND query ~ '^(SELECT|INSERT|UPDATE|DELETE|WITH)' ORDER BY total_exec_time DESC LIMIT 30) q`).Scan(&stats); e != nil {
			t.Fatal(e)
		}
		acceptanceReport(t, "decision-query-profile", stats)
	}
	report["started_at"] = started.UTC().Format(time.RFC3339Nano)
	report["api_finished_at"] = started.Add(apiWall).UTC().Format(time.RFC3339Nano)
	report["effects_finished_at"] = started.Add(drainedWall).UTC().Format(time.RFC3339Nano)
	report["configured_pools"] = map[string]int{"decision_api": 16, "screening_api": 4, "screening_worker": 4, "decision_worker": 4, "case_api": 8, "test_fixture": 16}
	report["decision_worker_configuration"] = map[string]any{"workflow_workers": 4, "outbox_workers": 8, "fetch_cooldown_ms": 5, "fetch_poll_ms": 100, "pool_max_connections": 4}
	acceptanceReport(t, "decision-performance", report)
	if len(ids) != requests || errorsCount.Load() != 0 || decisions != requests || evidence != requests || executions != requests || rules != requests*2 || workflowReceipts != workflows || eventReceipts != events || pendingWorkflow != 0 || pendingEvents != 0 || rescreens != monitors || failedJobs != 0 {
		n := 0
		for p := range problems {
			if n < 5 {
				t.Log(p)
			}
			n++
		}
		t.Fatal("end-to-end correctness, delivery or completeness mismatch; see report")
	}
}
