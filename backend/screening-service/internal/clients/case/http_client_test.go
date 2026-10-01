package caseclient

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticatedDelivery(t *testing.T) {
	for _, status := range []int{202, 401, 501} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer service-token" || r.Method != "POST" || r.URL.Path != "/v1/screening-events/reviewed" {
				t.Errorf("invalid integration request: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(status)
		}))
		err := NewHTTPClient(server.URL, "service-token", time.Second).PublishScreeningReviewed(context.Background(), ports.ScreeningReviewedCommand{})
		server.Close()
		if (err == nil) != (status == 202) {
			t.Fatalf("status=%d error=%v", status, err)
		}
	}
	for _, client := range []HTTPClient{NewHTTPClient("", "token", time.Second), NewHTTPClient("http://unused", "", time.Second)} {
		if err := client.PublishScreeningReviewed(context.Background(), ports.ScreeningReviewedCommand{}); err == nil {
			t.Fatal("missing integration configuration reported delivery")
		}
	}
}
