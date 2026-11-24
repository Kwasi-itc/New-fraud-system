package datamodel

import (
	"context"
	"fmt"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"net/http"
	"net/http/httptest"
	"strings"
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

	client := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "decision-engine-service"))

	first, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("first GetTenantModel failed: %v", err)
	}
	second, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111")
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

	client := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "decision-engine-service"))

	if _, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatalf("11111111-1111-4111-8111-111111111111 GetTenantModel failed: %v", err)
	}
	if _, err := client.GetTenantModel(context.Background(), "22222222-2222-4222-8222-222222222222"); err != nil {
		t.Fatalf("22222222-2222-4222-8222-222222222222 GetTenantModel failed: %v", err)
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

	client := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "decision-engine-service"))
	client.now = func() time.Time { return now }

	first, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("first GetTenantModel failed: %v", err)
	}

	now = now.Add(tenantModelCacheTTL + time.Nanosecond)
	second, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111")
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

	client := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "decision-engine-service"))

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, callers)
	revisions := make(chan string, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			model, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111")
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

func TestGetTenantModelReturnsDependencyErrorOnNon200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "decision-engine-service"))
	_, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111")
	if err == nil {
		t.Fatal("expected non-200 response to return an error")
	}
	if !strings.Contains(err.Error(), "unexpected status from data-model-service") {
		t.Fatalf("error = %v, want unexpected status from data-model-service", err)
	}
}
