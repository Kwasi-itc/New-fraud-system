package ingestion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecordIdentityAndPathEncoding(t *testing.T) {
	id := "synthetic/customer?scope=other#fragment"
	var escaped, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped, query = r.URL.EscapedPath(), r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"record": map[string]any{"object_id": id, "object_type": "customer", "fields": map[string]any{"name": "Synthetic"}}})
	}))
	defer server.Close()
	record, err := NewHTTPClient(server.URL, time.Second).GetRecord(context.Background(), "tenant", "customer", id)
	if err != nil || record.ObjectID != id {
		t.Fatal(record, err)
	}
	if query != "" || !strings.HasSuffix(escaped, "/synthetic%2Fcustomer%3Fscope=other%23fragment") {
		t.Fatal("record identifier changed request routing", escaped, query)
	}
}

func TestRecordRejectsWrongOrMissingIdentity(t *testing.T) {
	for _, body := range []string{
		`{"record":{"object_id":"other","object_type":"customer","fields":{"name":"Sensitive"}}}`,
		`{"record":{"object_id":"wanted","object_type":"other","fields":{"name":"Sensitive"}}}`,
		`{"record":{"fields":{"name":"Sensitive"}}}`, `{}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			record, err := NewHTTPClient(server.URL, time.Second).GetRecord(context.Background(), "tenant", "customer", "wanted")
			if err == nil || record.Fields != nil {
				t.Fatal("unverified record accepted", record, err)
			}
			if strings.Contains(err.Error(), "Sensitive") {
				t.Fatal("record data leaked")
			}
		})
	}
}

func TestRecordRejectsRedirectWithoutFollowing(t *testing.T) {
	calls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	_, err := NewHTTPClient(server.URL, time.Second).GetRecord(context.Background(), "tenant", "customer", "wanted")
	if err == nil || calls != 0 {
		t.Fatal("redirect followed", calls, err)
	}
}

func TestRecordTransportErrorDoesNotExposeIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	_, err := NewHTTPClient(server.URL, time.Second).GetRecord(context.Background(), "tenant", "customer", "private-object-id")
	if err == nil || strings.Contains(err.Error(), "private-object-id") || strings.Contains(err.Error(), server.URL) {
		t.Fatal("unsafe transport error", err)
	}
}
