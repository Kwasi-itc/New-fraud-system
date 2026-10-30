package datamodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/google/uuid"
)

func testContract(tenant, table, version uuid.UUID, name string) ports.PublishedContract {
	body, _ := json.Marshal(map[string]any{"id": table, "name": name, "fields": map[string]any{"amount": map[string]any{"name": "amount", "data_type": "float"}}})
	return ports.PublishedContract{TenantID: tenant, TableID: table, TableName: name, Version: version, TenantStatus: "active", Payload: body}
}

func TestReaderExplicitHTTPMode(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing private credential")
		}
		if status.Load() != 200 {
			w.WriteHeader(int(status.Load()))
			return
		}
		w.Header().Set("ETag", `"model-v1"`)
		fmt.Fprint(w, cacheTestBody)
	}))
	defer server.Close()
	cfg := ReaderConfig{Mode: "http", HTTPURL: server.URL, HTTPToken: "private-token", Timeout: time.Second, CacheEntries: 2, CacheBytes: 4096}
	forbidden := contractStoreFunc(func(context.Context, uuid.UUID, string, uuid.UUID, uuid.UUID) (ports.PublishedContract, error) {
		t.Error("HTTP mode consulted SQL")
		return ports.PublishedContract{}, errors.New("unexpected SQL")
	})
	reader, err := NewReader(cfg, forbidden)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reader.(ports.TableDataModelReader); ok {
		t.Fatal("full HTTP reader unexpectedly implements SQL table path")
	}
	tenant := uuid.New()
	if m, err := reader.GetPublishedDataModel(context.Background(), tenant); err != nil || m.TenantID != tenant || !m.Writable {
		t.Fatal(m, err)
	}
	for _, code := range []int{401, 403, 503} {
		status.Store(int32(code))
		if _, err := reader.GetPublishedDataModel(context.Background(), tenant); err == nil {
			t.Fatal("warm cache bypassed origin error", code)
		}
	}
	if calls.Load() != 4 {
		t.Fatal(calls.Load())
	}
}

func TestReaderHTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	reader, err := NewReader(ReaderConfig{Mode: "http", HTTPURL: server.URL, HTTPToken: "private-token", Timeout: 50 * time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetPublishedDataModel(context.Background(), uuid.New()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestReaderSQLNeverFallsBackToConfiguredHTTP(t *testing.T) {
	var httpCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpCalls.Add(1); fmt.Fprint(w, cacheTestBody) }))
	defer server.Close()
	tenant, table, version := uuid.New(), uuid.New(), uuid.New()
	state := testContract(tenant, table, version, "records")
	var fail error
	store := contractStoreFunc(func(context.Context, uuid.UUID, string, uuid.UUID, uuid.UUID) (ports.PublishedContract, error) {
		return state, fail
	})
	reader, err := NewReader(ReaderConfig{Mode: "sql", HTTPURL: server.URL, HTTPToken: "private-token", Timeout: time.Second, CacheEntries: 2, CacheBytes: 4096}, store)
	if err != nil {
		t.Fatal(err)
	}
	tableReader := reader.(ports.TableDataModelReader)
	if _, err := tableReader.GetPublishedTable(context.Background(), tenant, "records"); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"database unavailable", "publication gate closed", "authorization revoked"} {
		fail = errors.New(reason)
		if _, err := tableReader.GetPublishedTable(context.Background(), tenant, "records"); !errors.Is(err, fail) {
			t.Fatal(err)
		}
	}
	fail = nil
	state.Payload = []byte("{")
	if _, err := tableReader.GetPublishedTable(context.Background(), tenant, "records"); err == nil {
		t.Fatal("malformed SQL payload accepted")
	}
	if httpCalls.Load() != 0 {
		t.Fatal("SQL failure called HTTP")
	}
	if _, err := reader.GetPublishedDataModel(context.Background(), tenant); err != nil || httpCalls.Load() != 1 {
		t.Fatal("explicit full-model call", err)
	}
	sqlOnly, err := NewReader(ReaderConfig{Mode: "sql"}, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlOnly.GetPublishedDataModel(context.Background(), tenant); err == nil {
		t.Fatal("missing full-model provider accepted")
	}
	if _, err := NewContractReader(nil, nil, 0, 0).GetPublishedTable(context.Background(), tenant, "records"); err == nil {
		t.Fatal("nil store must fail closed")
	}
}

func TestReaderRecreatedTableAndPublicationVersion(t *testing.T) {
	tenant, table, version := uuid.New(), uuid.New(), uuid.New()
	state := testContract(tenant, table, version, "records")
	var knownID, knownVersion uuid.UUID
	store := contractStoreFunc(func(_ context.Context, _ uuid.UUID, _ string, id, v uuid.UUID) (ports.PublishedContract, error) {
		knownID, knownVersion = id, v
		return state, nil
	})
	r := NewContractReader(store, nil, 2, 4096)
	first, err := r.GetPublishedTable(context.Background(), tenant, "records")
	if err != nil || first.PublicationVersion != version {
		t.Fatal(first, err)
	}
	// Same name/version but new stable identity cannot reuse the cached table.
	replacement := uuid.New()
	state.TableID = replacement
	state.Payload = nil
	if _, err := r.GetPublishedTable(context.Background(), tenant, "records"); err == nil {
		t.Fatal("recreated table reused old representation")
	}
	if knownID != table || knownVersion != version {
		t.Fatal("wrong cache revalidation identity")
	}
	state = testContract(tenant, replacement, version, "records")
	next, err := r.GetPublishedTable(context.Background(), tenant, "records")
	if err != nil || next.Tables["records"].ID != replacement || next.PublicationVersion != version {
		t.Fatal(next, err)
	}
	state.Payload = nil
	hit, err := r.GetPublishedTable(context.Background(), tenant, "records")
	if err != nil || hit.PublicationVersion != version || knownID != replacement {
		t.Fatal(hit, err)
	}
	state.TableID = uuid.Nil
	state.Version = uuid.Nil
	missing, err := r.GetPublishedTable(context.Background(), tenant, "records")
	if err != nil || len(missing.Tables) != 0 {
		t.Fatal("deleted table served from cache", missing, err)
	}
}

func TestSQLCacheBoundedConcurrentRevalidation(t *testing.T) {
	var calls atomic.Int32
	store := contractStoreFunc(func(_ context.Context, id uuid.UUID, name string, knownID, knownVersion uuid.UUID) (ports.PublishedContract, error) {
		calls.Add(1)
		table := uuid.NewSHA1(id, []byte(name))
		version := uuid.NewSHA1(table, []byte("v1"))
		state := testContract(id, table, version, name)
		if knownID == table && knownVersion == version {
			state.Payload = nil
		}
		return state, nil
	})
	r := NewContractReader(store, nil, 2, 1024)
	tenants := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				tenant := tenants[(i+j)%len(tenants)]
				m, err := r.GetPublishedTable(context.Background(), tenant, "records")
				if err != nil || m.TenantID != tenant || m.Tables["records"].ID != uuid.NewSHA1(tenant, []byte("records")) {
					t.Error("invalid concurrent model", err)
					return
				}
				delete(m.Tables["records"].Fields, "amount")
			}
		}(i)
	}
	wg.Wait()
	if calls.Load() != 160 {
		t.Fatal("cache bypassed revalidation", calls.Load())
	}
	if len(r.cache.entries) > 2 || r.cache.bytes > 1024 || r.cache.bytes < 0 {
		t.Fatal("cache bounds", r.cache.bytes, len(r.cache.entries))
	}
	disabled := NewContractReader(store, nil, 0, 0)
	for i := 0; i < 2; i++ {
		if _, err := disabled.GetPublishedTable(context.Background(), tenants[0], "records"); err != nil {
			t.Fatal(err)
		}
	}
	if len(disabled.cache.entries) != 0 {
		t.Fatal("disabled SQL cache retained model")
	}
	c := newModelCache(2, 1024)
	for _, size := range []int{-1, 0, 1025} {
		c.put(cacheEntry{tenantID: tenants[0], encodedBytes: size})
	}
	if c.bytes != 0 || len(c.entries) != 0 {
		t.Fatal("invalid byte accounting accepted")
	}
}

func TestReaderRejectsIncompleteConfiguration(t *testing.T) {
	valid := ReaderConfig{Mode: "http", HTTPURL: "https://data-model.internal", HTTPToken: "private", Timeout: time.Second, CacheEntries: 2, CacheBytes: 4096}
	for _, change := range []func(*ReaderConfig){
		func(c *ReaderConfig) { c.Mode = "auto" }, func(c *ReaderConfig) { c.HTTPURL = "" },
		func(c *ReaderConfig) { c.HTTPURL = "https://user:pass@host" }, func(c *ReaderConfig) { c.HTTPURL = "file:///etc/config" },
		func(c *ReaderConfig) { c.HTTPToken = "" }, func(c *ReaderConfig) { c.HTTPToken = "bad\nheader" },
		func(c *ReaderConfig) { c.Timeout = 0 }, func(c *ReaderConfig) { c.CacheEntries = -1 },
		func(c *ReaderConfig) { c.CacheBytes = 65 << 20 }, func(c *ReaderConfig) { c.CacheEntries = 0 },
		func(c *ReaderConfig) { c.Mode = "sql" },
	} {
		c := valid
		change(&c)
		if _, err := NewReader(c, nil); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestCacheAbsoluteReuseLifetime(t *testing.T) {
	c := newModelCache(2, 1024)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	id := uuid.New()
	c.put(cacheEntry{tenantID: id, encodedBytes: 100})
	now = now.Add(modelCacheMaxAge - time.Nanosecond)
	if _, ok := c.get(id); !ok {
		t.Fatal("expired before boundary")
	}
	now = now.Add(time.Nanosecond)
	if _, ok := c.get(id); ok {
		t.Fatal("hit extended absolute lifetime")
	}
	if c.bytes != 0 || len(c.entries) != 0 {
		t.Fatal("expired entry retained after lookup")
	}
	c.put(cacheEntry{tenantID: id, encodedBytes: 100})
	if _, ok := c.get(id); !ok {
		t.Fatal("fresh insertion not reusable")
	}
}
