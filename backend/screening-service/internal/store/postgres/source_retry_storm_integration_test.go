package postgres_test

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceRetryStormIsLeasedBoundedAndDoesNotInventFreshness(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	spec, _ := sourceconnectors.Lookup("ofac-sdn")
	key := sourceingest.Key{Dataset: "retry-fixture"}
	if e := (sourcecollector.Pipeline{DB: f.db}).Register(ctx, key, spec); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(ctx, `UPDATE screening.source_connections SET enabled=true,rights_state='approved',rights_review_reference='fictional retry fixture' WHERE dataset_id=$1`, key.Dataset); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int64
	collector := sourcecollector.Collector{DB: f.db, Dir: t.TempDir(), Fetcher: sourceconnectors.Fetcher{Client: &http.Client{Transport: sourceRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return nil, errors.New("injected transient source failure")
	})}}}
	burst := func() {
		t.Helper()
		var group sync.WaitGroup
		for i := 0; i < 12; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				if _, e := collector.RunOne(ctx); e != nil {
					t.Error(e)
				}
			}()
		}
		group.Wait()
	}
	burst()
	if calls.Load() != 1 {
		t.Fatal("duplicate lease during retry storm", calls.Load())
	}
	burst()
	if calls.Load() != 1 {
		t.Fatal("persisted backoff bypassed", calls.Load())
	}
	for i := 0; i < 4; i++ {
		if _, e := f.db.Exec(ctx, `UPDATE screening.source_connections SET next_check_at=now() WHERE dataset_id=$1`, key.Dataset); e != nil {
			t.Fatal(e)
		}
		burst()
	}
	var failures int
	var enabled, ready bool
	var diagnostic string
	if e := f.db.QueryRow(ctx, `SELECT c.failures,c.enabled,r.ready,c.last_error FROM screening.source_connections c JOIN screening.source_readiness_v1 r USING(owner_scope,dataset_id) WHERE c.dataset_id=$1`, key.Dataset).Scan(&failures, &enabled, &ready, &diagnostic); e != nil || failures != 5 || enabled || ready || diagnostic != "quarantined_retry_exhausted" {
		t.Fatal(failures, enabled, ready, diagnostic, e)
	}
	burst()
	if calls.Load() != 5 {
		t.Fatal("exhausted retries continued", calls.Load())
	}
	var count int
	if e := f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_publications WHERE dataset_id=$1`, key.Dataset).Scan(&count); e != nil || count != 0 {
		t.Fatal("failed download published", count, e)
	}
}
