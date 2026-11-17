package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
)

// Explicit workload, not a matching-quality corpus or a production TPS claim.
// It creates/drops its own database via the normal disposable fixture.
func TestSourceWorkloadConcurrentPublicationAndLargeCatalog(t *testing.T) {
	if os.Getenv("SCREENING_RUN_LOAD_TEST") != "true" {
		t.Skip("set SCREENING_RUN_LOAD_TEST=true for the 40,000-record workload")
	}
	f := newInternalFixture(t)
	ctx := context.Background()
	const count = 40000
	const requests = 1000
	concurrency := 8
	if raw := os.Getenv("SCREENING_WORKLOAD_CONCURRENCY"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 32 {
			t.Fatal("invalid workload concurrency")
		}
		concurrency = v
	}
	names := make([]string, count)
	records := make([]sourcedata.Record, count)
	for i := range records {
		sum := sha256.Sum256([]byte(fmt.Sprintf("synthetic-workload-v1:%d", i)))
		words := []string{}
		for word := 0; word < 4; word++ {
			var b strings.Builder
			for letter := 0; letter < 6; letter++ {
				b.WriteByte('a' + sum[word*6+letter]%26)
			}
			words = append(words, b.String())
		}
		names[i] = strings.Join(words, " ")
		records[i] = sourcedata.Record{ID: fmt.Sprintf("load-%06d", i), Name: names[i], Type: "Person", SourceReference: "Fictional mechanical load fixture; no identity labels"}
	}
	envelope := func() []byte {
		raw, e := json.Marshal(struct {
			Version int                 `json:"version"`
			Source  string              `json:"source"`
			Parser  string              `json:"parser"`
			Records []sourcedata.Record `json:"records"`
		}{2, "tenant-upload", "load-fixture-v1", records})
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	ingest := sourceingest.Store{DB: f.db}
	started := time.Now()
	first, e := ingest.Stage(ctx, f.private, bytes.NewReader(envelope()))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, f.private, first); e != nil {
		t.Fatal(e)
	}
	ingestTime := time.Since(started)
	if _, e = f.db.Exec(ctx, `ANALYZE screening.source_names;ANALYZE screening.source_records`); e != nil {
		t.Fatal(e)
	}
	var plan []byte
	if e = f.db.QueryRow(ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) SELECT record_id FROM screening.source_names WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3 AND normalized=$4`, f.private.Owner, f.private.Dataset, first, sourcedata.Normalize(names[0])).Scan(&plan); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(plan, []byte("Index")) {
		t.Fatal("large exact lookup did not use an index", string(plan))
	}
	probeRequest := internalRequest(names[0], "Person", sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset})
	statement, planArgs, e := internalprovider.CandidateStatement([]internalprovider.CandidateSelection{{Owner: f.private.Owner, Dataset: f.private.Dataset, Generation: first}}, probeRequest.Queries)
	if e != nil {
		t.Fatal(e)
	}
	planTx, e := f.db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = planTx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.15',true)`); e != nil {
		t.Fatal(e)
	}
	var fuzzyPlan json.RawMessage
	if e = planTx.QueryRow(ctx, "EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) "+statement, planArgs...).Scan(&fuzzyPlan); e != nil {
		t.Fatal(e)
	}
	if e = planTx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("SCREENING_WORKLOAD_PLAN_ONLY") == "true" {
		if output := os.Getenv("SCREENING_WORKLOAD_REPORT"); output != "" {
			if e = os.WriteFile(output, fuzzyPlan, 0600); e != nil {
				t.Fatal(e)
			}
		}
		t.Log("plan-only diagnostic; no throughput measurements")
		return
	}
	provider := internalprovider.Provider{DB: f.db}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}
	// A full second generation stages/publishes while the exact/fuzzy traffic runs.
	// A response may bind either complete published snapshot, never a half import.
	records[count-1].Name = "Changed Fictional Load Record"
	secondInput := envelope()
	publication := make(chan error, 1)
	var nextGeneration atomic.Value
	go func() {
		g, e := ingest.Stage(ctx, f.private, bytes.NewReader(secondInput))
		if e == nil {
			_, e = ingest.Publish(ctx, f.private, g)
			if e == nil {
				nextGeneration.Store(g.String())
			}
		}
		publication <- e
	}()
	latencies := make([]time.Duration, requests)
	var failures, partial, missing atomic.Int64
	work := make(chan int, requests)
	for i := 0; i < requests; i++ {
		work <- i
	}
	close(work)
	var wg sync.WaitGroup
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	searchStarted := time.Now()
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range work {
				index := (n * 97) % (count - 1)
				request := internalRequest(names[index], "Person", ref)
				if n%2 == 1 {
					request.ProviderConfig, _ = json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.FuzzyProfile, Threshold: 85, Datasets: []sourcedata.DatasetRef{ref}})
				}
				start := time.Now()
				result, e := provider.Search(ctx, f.a, request)
				latencies[n] = time.Since(start)
				if e != nil {
					failures.Add(1)
					continue
				}
				if result.Partial {
					partial.Add(1)
				}
				found := false
				for _, match := range result.Matches {
					var candidate struct {
						Ref sourcedata.CandidateRef `json:"provenance"`
					}
					if json.Unmarshal(match.Payload, &candidate) == nil && candidate.Ref.RecordID == fmt.Sprintf("load-%06d", index) {
						found = true
					}
				}
				if !found {
					missing.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	searchElapsed := time.Since(searchStarted)
	runtime.ReadMemStats(&after)
	if e = <-publication; e != nil {
		t.Fatal("concurrent publication", e)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	percentile := func(p int) float64 { return float64(latencies[(len(latencies)-1)*p/100].Microseconds()) / 1000 }
	report := map[string]any{"dataset_records": count, "requests": requests, "concurrency": concurrency, "gomaxprocs": runtime.GOMAXPROCS(0), "go_version": runtime.Version(), "profiles": []string{sourcedata.Profile, sourcedata.FuzzyProfile}, "p50_ms": percentile(50), "p95_ms": percentile(95), "p99_ms": percentile(99), "search_throughput_per_second": float64(requests) / searchElapsed.Seconds(), "search_wall_ms": searchElapsed.Milliseconds(), "ingest_publish_ms": ingestTime.Milliseconds(), "ingest_records_per_second": float64(count) / ingestTime.Seconds(), "errors": failures.Load(), "partial": partial.Load(), "missing_expected_source_ids": missing.Load(), "go_heap_inuse_after_bytes": after.HeapInuse, "go_heap_inuse_before_bytes": before.HeapInuse, "go_total_alloc_delta_bytes": after.TotalAlloc - before.TotalAlloc, "published_second_generation": nextGeneration.Load(), "exact_lookup_index_verified": true, "dataset_type": "mechanical synthetic names, not labelled accuracy or real-source distribution", "production_throughput_claim": false, "concurrent_full_import": true, "scope": "in-process provider and PostgreSQL, excludes API/queue/rules"}
	report["database_pool_max_connections"] = f.db.Stat().MaxConns()
	raw, _ := json.Marshal(report)
	t.Log("SOURCE_WORKLOAD " + string(raw))
	if output := os.Getenv("SCREENING_WORKLOAD_REPORT"); output != "" {
		if e = os.WriteFile(output+".plan.json", fuzzyPlan, 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(output, append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if failures.Load() != 0 || partial.Load() != 0 || missing.Load() != 0 {
		t.Fatal("load correctness/completeness failed")
	}
}
