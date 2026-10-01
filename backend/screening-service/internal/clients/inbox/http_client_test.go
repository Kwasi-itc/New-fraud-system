package inbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticatedInboxContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-token" || r.URL.Path != "/internal/v1/tenants/tenant/inboxes/inbox" {
			t.Errorf("invalid request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"inbox":{"id":"inbox"}}`))
	}))
	defer server.Close()
	if _, err := NewHTTPClient(server.URL, "service-token", time.Second).GetInbox(context.Background(), "tenant", "inbox"); err != nil {
		t.Fatal(err)
	}
	for _, client := range []HTTPClient{NewHTTPClient("", "token", time.Second), NewHTTPClient(server.URL, "", time.Second)} {
		if _, err := client.GetInbox(context.Background(), "tenant", "inbox"); err == nil {
			t.Fatal("missing configuration accepted")
		}
	}
}
