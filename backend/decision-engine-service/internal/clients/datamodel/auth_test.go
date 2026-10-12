package datamodel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrivateDataModelCredentialAndRedirectBoundary(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-data-model-token" {
			w.WriteHeader(401)
			return
		}
		received.Store(true)
		fmt.Fprint(w, `{"data_model":{"revision_id":"test","ingestion_contract":{"record_lookup_field":"object_id"},"tables":{}}}`)
	}))
	defer server.Close()
	client := NewHTTPClient(server.URL, time.Second).WithAuthToken("private-data-model-token")
	if _, err := client.GetTenantModel(context.Background(), "tenant"); err != nil {
		t.Fatal(err)
	}
	if !received.Load() {
		t.Fatal("private integration credential was not sent")
	}
	var followed atomic.Bool
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Store(true); w.WriteHeader(200) }))
	defer redirect.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, redirect.URL, 307) }))
	defer origin.Close()
	client = NewHTTPClient(origin.URL, time.Second).WithAuthToken("private-data-model-token")
	if _, err := client.GetTenantModel(context.Background(), "tenant"); err == nil {
		t.Fatal("redirect unexpectedly succeeded")
	}
	if followed.Load() {
		t.Fatal("credential-bearing redirect was followed")
	}
}
