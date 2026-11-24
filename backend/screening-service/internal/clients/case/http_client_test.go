package caseclient

import (
	"context"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedDelivery(t *testing.T) {
	signer := authtest.ServiceSigner(t, "screening-service")
	verifier := authtest.ServiceVerifier(t, signer, "urn:fraud:case-manager-service")
	for _, status := range []int{202, 401, 501} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := verifier.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if err != nil || !p.Can("cases:events-write") || r.Method != "POST" || r.URL.Path != "/v1/screening-events/reviewed" {
				t.Errorf("invalid integration request: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(status)
		}))
		err := NewHTTPClient(server.URL, time.Second).WithSigner(signer).PublishScreeningReviewed(context.Background(), ports.ScreeningReviewedCommand{TenantID: "11111111-1111-4111-8111-111111111111"})
		server.Close()
		if (err == nil) != (status == 202) {
			t.Fatalf("status=%d error=%v", status, err)
		}
	}
	for _, client := range []HTTPClient{NewHTTPClient("", time.Second).WithSigner(signer), NewHTTPClient("http://unused", time.Second).WithSigner(signer)} {
		if err := client.PublishScreeningReviewed(context.Background(), ports.ScreeningReviewedCommand{TenantID: "11111111-1111-4111-8111-111111111111"}); err == nil {
			t.Fatal("missing integration configuration reported delivery")
		}
	}
}

func TestCallbackErrorsDoNotExposeEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, endpoint := range []string{"http://private.invalid/private-secret", "http://%private-secret"} {
		err := NewHTTPClient(endpoint, time.Second).WithSigner(authtest.ServiceSigner(t, "screening-service")).PublishScreeningReviewed(ctx, ports.ScreeningReviewedCommand{TenantID: "11111111-1111-4111-8111-111111111111"})
		if err == nil || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "private.invalid") {
			t.Fatalf("callback endpoint leaked: %v", err)
		}
	}
}
