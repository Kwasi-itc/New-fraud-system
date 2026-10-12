package datamodel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

const cacheTestBody = `{"data_model":{"revision_id":"rev","ingestion_contract":{"tenant_status":"active","writable":true,"managed_system_fields":["updated_at"],"record_lookup_field":"object_id"},"tables":{"records":{"fields":{"status":{"enum_values":[{"value":"open"}]}}}}}}`

func TestCacheRevalidatesEveryRequestAndIsolatesCallers(t *testing.T) {
	var calls, hits atomic.Int32
	var suspended atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body := cacheTestBody
		if suspended.Load() {
			body = `{"data_model":{"revision_id":"rev","ingestion_contract":{"writable":false},"tables":{}}}`
		}
		tag := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(body)))
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			hits.Add(1)
			w.WriteHeader(304)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	client := NewHTTPClient(server.URL, time.Second)
	id := uuid.New()
	first, err := client.GetPublishedDataModel(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	first.ManagedSystemFields[0] = "corrupted"
	first.Tables["records"].Fields["status"].EnumValues[0].Value = "corrupted"
	delete(first.Tables, "records")
	second, err := client.GetPublishedDataModel(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if second.ManagedSystemFields[0] != "updated_at" || second.Tables["records"].Fields["status"].EnumValues[0].Value != "open" {
		t.Fatal("caller mutated cache")
	}
	second.Tables["records"].Fields["status"].EnumValues[0].Value = "corrupted again"
	third, err := client.GetPublishedDataModel(context.Background(), id)
	if err != nil || third.Tables["records"].Fields["status"].EnumValues[0].Value != "open" {
		t.Fatalf("hit mutated cache: %v", err)
	}
	suspended.Store(true)
	model, err := client.GetPublishedDataModel(context.Background(), id)
	if err != nil || model.Writable {
		t.Fatalf("suspension not observed: %v", err)
	}
	if calls.Load() != 4 || hits.Load() != 2 {
		t.Fatalf("calls=%d hits=%d", calls.Load(), hits.Load())
	}
}

func TestCacheNeverFallsBackOnErrors(t *testing.T) {
	for _, failure := range []string{"unavailable", "malformed", "bad304", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			var fail atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if fail.Load() {
					switch failure {
					case "unavailable":
						w.WriteHeader(503)
					case "malformed":
						fmt.Fprint(w, `{`)
					case "bad304":
						w.Header().Set("ETag", `"wrong"`)
						w.WriteHeader(304)
					}
					return
				}
				w.Header().Set("ETag", `"valid"`)
				fmt.Fprint(w, cacheTestBody)
			}))
			defer server.Close()
			client := NewHTTPClient(server.URL, time.Second)
			id := uuid.New()
			if _, err := client.GetPublishedDataModel(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			fail.Store(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "canceled" {
				cancel()
			}
			if _, err := client.GetPublishedDataModel(ctx, id); err == nil {
				t.Fatal("must fail closed")
			}
		})
	}
}

func TestCacheTenantIsolationAndConcurrentAccess(t *testing.T) {
	var leaked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(r.URL.Path)))
		if condition := r.Header.Get("If-None-Match"); condition != "" && condition != tag {
			leaked.Store(true)
		}
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(304)
			return
		}
		fmt.Fprint(w, cacheTestBody)
	}))
	defer server.Close()
	client := NewHTTPClient(server.URL, 5*time.Second)
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				model, err := client.GetPublishedDataModel(context.Background(), ids[i%2])
				if err != nil {
					t.Error(err)
					return
				}
				if model.TenantID != ids[i%2] {
					t.Error("wrong tenant")
				}
				delete(model.Tables, "records")
			}
		}(i)
	}
	wg.Wait()
	if leaked.Load() {
		t.Fatal("cross-tenant validator")
	}
}

func TestCacheBoundsAndLRU(t *testing.T) {
	c := newModelCache(2, 10)
	a, b, d := uuid.New(), uuid.New(), uuid.New()
	c.put(cacheEntry{tenantID: a, encodedBytes: 4})
	c.put(cacheEntry{tenantID: b, encodedBytes: 4})
	c.get(a)
	c.put(cacheEntry{tenantID: d, encodedBytes: 4})
	if _, ok := c.get(b); ok {
		t.Fatal("least recently used entry retained")
	}
	c.put(cacheEntry{tenantID: d, encodedBytes: 8})
	if _, ok := c.get(a); ok {
		t.Fatal("byte budget exceeded")
	}
	c.put(cacheEntry{tenantID: d, encodedBytes: 11})
	if _, ok := c.get(d); ok {
		t.Fatal("oversized entry retained")
	}
	if c.bytes != 0 {
		t.Fatalf("retained bytes=%d", c.bytes)
	}
}

func TestCacheNoStoreAndOlderServers(t *testing.T) {
	for _, policy := range []string{"no-store", "no-etag"} {
		t.Run(policy, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if call == 3 && r.Header.Get("If-None-Match") != "" {
					t.Error("uncacheable response retained")
				}
				if call == 1 || policy != "no-etag" {
					w.Header().Set("ETag", `"valid"`)
				}
				if call > 1 && policy == "no-store" {
					w.Header().Set("Cache-Control", "no-store")
				}
				fmt.Fprint(w, cacheTestBody)
			}))
			defer server.Close()
			client := NewHTTPClient(server.URL, time.Second)
			id := uuid.New()
			for i := 0; i < 3; i++ {
				if _, err := client.GetPublishedDataModel(context.Background(), id); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCacheRejectsUnsolicitedNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(304) }))
	defer server.Close()
	client := NewHTTPClient(server.URL, time.Second)
	if _, err := client.GetPublishedDataModel(context.Background(), uuid.New()); err == nil {
		t.Fatal("304 without a cached representation must fail")
	}
}

func TestCacheDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			t.Error("disabled cache sent validator")
		}
		w.Header().Set("ETag", `"valid"`)
		fmt.Fprint(w, cacheTestBody)
	}))
	defer server.Close()
	client := NewHTTPClientWithCacheLimits(server.URL, time.Second, 0, 0)
	id := uuid.New()
	for i := 0; i < 2; i++ {
		if _, err := client.GetPublishedDataModel(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if client.cache.lru.Len() != 0 {
		t.Fatal("disabled cache retained data")
	}
}
