package inbox

import (
	"context"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedInboxContract(t *testing.T) {
	signer := authtest.ServiceSigner(t, "screening-service")
	verifier := authtest.ServiceVerifier(t, signer, "urn:fraud:case-manager-service")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := verifier.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil || !p.Can("inboxes:read") || r.URL.Path != "/internal/v1/tenants/11111111-1111-4111-8111-111111111111/inboxes/inbox" {
			t.Errorf("invalid request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"inbox":{"id":"inbox"}}`))
	}))
	defer server.Close()
	if _, err := NewHTTPClient(server.URL, time.Second).WithSigner(signer).GetInbox(context.Background(), "11111111-1111-4111-8111-111111111111", "inbox"); err != nil {
		t.Fatal(err)
	}
	for _, client := range []HTTPClient{NewHTTPClient("", time.Second).WithSigner(authtest.ServiceSigner(t, "screening-service"))} {
		if _, err := client.GetInbox(context.Background(), "11111111-1111-4111-8111-111111111111", "inbox"); err == nil {
			t.Fatal("missing configuration accepted")
		}
	}
}
