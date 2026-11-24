package decisionengine

import (
	"context"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

func TestCallbackMissingCredentialDoesNotSend(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	defer server.Close()
	for range 3 {
		err := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "screening-service")).PublishScreeningStatusChanged(context.Background(), ports.ScreeningStatusChangedCommand{})
		if err == nil {
			t.Fatal("invalid callback credential accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid credentials made outbound calls")
	}
}

func TestHTTPClientPublishesAuthorizedStatusCallback(t *testing.T) {
	t.Parallel()
	signer := authtest.ServiceSigner(t, "screening-service")
	verifier := authtest.ServiceVerifier(t, signer, "urn:fraud:decision-engine-service")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/screening-status-updates" {
			t.Fatalf("path = %q, want /internal/screening-status-updates", r.URL.Path)
		}
		if p, err := verifier.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); err != nil || !p.Can("screening:callback") {
			t.Error("invalid signed callback")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second).WithSigner(signer)
	err := client.PublishScreeningStatusChanged(context.Background(), ports.ScreeningStatusChangedCommand{
		TenantID:          "11111111-1111-4111-8111-111111111111",
		ScreeningID:       "screening-1",
		DecisionID:        "decision-1",
		ScreeningConfigID: "cfg-1",
		Status:            "awaiting_review",
		Provider:          "opensanctions",
		ObjectType:        "company",
		ObjectID:          "obj-1",
	})
	if err != nil {
		t.Fatalf("PublishScreeningStatusChanged() error = %v", err)
	}
}

func TestCallbackRedirectsAndErrorsAreSafe(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); w.WriteHeader(204) }))
	defer target.Close()
	for _, status := range []int{307, 401, 429, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
			_, _ = w.Write([]byte("private-secret-response"))
		}))
		err := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "screening-service")).PublishScreeningStatusChanged(context.Background(), ports.ScreeningStatusChangedCommand{})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("unsafe status %d: %v", status, err)
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("callback followed redirect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, endpoint := range []string{"http://private.invalid/private-secret", "http://%private-secret"} {
		err := NewHTTPClient(endpoint, time.Second).WithSigner(authtest.ServiceSigner(t, "screening-service")).PublishScreeningStatusChanged(ctx, ports.ScreeningStatusChangedCommand{})
		if err == nil || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "private.invalid") {
			t.Fatalf("callback endpoint leaked: %v", err)
		}
	}
}
