package datamodel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetTenantModelUsesCacheWithinTTL(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"data_model":{"revision_id":"rev-%d","ingestion_contract":{"record_lookup_field":"object_id"},"tables":{}}}`, calls)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)

	first, err := client.GetTenantModel(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("first GetTenantModel failed: %v", err)
	}
	second, err := client.GetTenantModel(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("second GetTenantModel failed: %v", err)
	}

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if first.RevisionID != "rev-1" || second.RevisionID != "rev-1" {
		t.Fatalf("cached revisions = %q and %q, want rev-1", first.RevisionID, second.RevisionID)
	}
}

func TestGetTenantModelCacheSeparatesTenants(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"data_model":{"revision_id":"rev-%d","ingestion_contract":{"record_lookup_field":"object_id"},"tables":{}}}`, calls)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)

	if _, err := client.GetTenantModel(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("tenant-1 GetTenantModel failed: %v", err)
	}
	if _, err := client.GetTenantModel(context.Background(), "tenant-2"); err != nil {
		t.Fatalf("tenant-2 GetTenantModel failed: %v", err)
	}

	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestGetTenantModelRefreshesExpiredCache(t *testing.T) {
	t.Parallel()

	var calls int
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"data_model":{"revision_id":"rev-%d","ingestion_contract":{"record_lookup_field":"object_id"},"tables":{}}}`, calls)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	client.now = func() time.Time { return now }

	first, err := client.GetTenantModel(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("first GetTenantModel failed: %v", err)
	}

	now = now.Add(tenantModelCacheTTL + time.Nanosecond)
	second, err := client.GetTenantModel(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("second GetTenantModel failed: %v", err)
	}

	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if first.RevisionID != "rev-1" || second.RevisionID != "rev-2" {
		t.Fatalf("revisions = %q and %q, want rev-1 and rev-2", first.RevisionID, second.RevisionID)
	}
}

func TestGetTenantModelCoalescesConcurrentRefreshes(t *testing.T) {
	t.Parallel()

	const callers = 50

	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			<-release
		}
		fmt.Fprintf(w, `{"data_model":{"revision_id":"rev-%d","ingestion_contract":{"record_lookup_field":"object_id"},"tables":{}}}`, call)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, callers)
	revisions := make(chan string, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			model, err := client.GetTenantModel(context.Background(), "tenant-1")
			if err != nil {
				errs <- err
				return
			}
			revisions <- model.RevisionID
		}()
	}

	close(start)
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errs)
	close(revisions)

	for err := range errs {
		t.Fatalf("GetTenantModel failed: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	for revision := range revisions {
		if revision != "rev-1" {
			t.Fatalf("revision = %q, want rev-1", revision)
		}
	}
}
