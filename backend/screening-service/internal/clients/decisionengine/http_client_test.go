package decisionengine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
)

func TestHTTPClientPublishesAuthorizedStatusCallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/screening-status-updates" {
			t.Fatalf("path = %q, want /internal/screening-status-updates", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("authorization header = %q, want Bearer token-1", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "token", "token-1", time.Second)
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
