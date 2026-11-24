package datamodel

import (
	"context"
	"fmt"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrivateDataModelCredentialAndRedirectBoundary(t *testing.T) {
	var received atomic.Bool
	signer := authtest.ServiceSigner(t, "decision-engine-service")
	verifier := authtest.ServiceVerifier(t, signer, "urn:fraud:data-model-service")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := verifier.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil || !p.Can("models:read") || !p.IsService() {
			w.WriteHeader(401)
			return
		}
		received.Store(true)
		fmt.Fprint(w, `{"data_model":{"revision_id":"test","ingestion_contract":{"record_lookup_field":"object_id"},"tables":{}}}`)
	}))
	defer server.Close()
	client := NewHTTPClient(server.URL, time.Second).WithSigner(signer)
	if _, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111"); err != nil {
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
	client = NewHTTPClient(origin.URL, time.Second).WithSigner(signer)
	if _, err := client.GetTenantModel(context.Background(), "11111111-1111-4111-8111-111111111111"); err == nil {
		t.Fatal("redirect unexpectedly succeeded")
	}
	if followed.Load() {
		t.Fatal("credential-bearing redirect was followed")
	}
}
