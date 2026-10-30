package decisionengine

import (
	"context"
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
	for _, tc := range []struct{ mode, token string }{{"disabled", strings.Repeat("x", 32)}, {"token", ""}, {"token", "short"}} {
		err := NewHTTPClient(server.URL, tc.mode, tc.token, time.Second).PublishScreeningStatusChanged(context.Background(), ports.ScreeningStatusChangedCommand{})
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/screening-status-updates" {
			t.Fatalf("path = %q, want /internal/screening-status-updates", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer synthetic-callback-private-token-32bytes" {
			t.Fatalf("authorization header = %q, want Bearer synthetic-callback-private-token-32bytes", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "token", "synthetic-callback-private-token-32bytes", time.Second)
	err := client.PublishScreeningStatusChanged(context.Background(), ports.ScreeningStatusChangedCommand{
		TenantID:          "tenant-1",
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
		err := NewHTTPClient(server.URL, "token", "synthetic-callback-private-token-32bytes", time.Second).PublishScreeningStatusChanged(context.Background(), ports.ScreeningStatusChangedCommand{})
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
		err := NewHTTPClient(endpoint, "token", "synthetic-callback-private-token-32bytes", time.Second).PublishScreeningStatusChanged(ctx, ports.ScreeningStatusChangedCommand{})
		if err == nil || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "private.invalid") {
			t.Fatalf("callback endpoint leaked: %v", err)
		}
	}
}
