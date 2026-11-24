package ingestion

import (
	"context"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestHTTPClientGetRecordReturnsDependencyErrorOnNon200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second).WithSigner(authtest.ServiceSigner(t, "decision-engine-service"))
	_, err := client.GetRecord(context.Background(), "11111111-1111-4111-8111-111111111111", "transactions", "txn-1")
	if err == nil {
		t.Fatal("expected non-200 response to return an error")
	}
	if !strings.Contains(err.Error(), "unexpected status from ingestion-service") {
		t.Fatalf("error = %v, want unexpected status from ingestion-service", err)
	}
}

func TestHTTPClientAggregateRecordsReturnsTimeoutError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value": 1}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 10*time.Millisecond).WithSigner(authtest.ServiceSigner(t, "decision-engine-service"))
	_, err := client.AggregateRecords(context.Background(), "11111111-1111-4111-8111-111111111111", ports.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
	})
	if err == nil {
		t.Fatal("expected timeout to return an error")
	}
	if !strings.Contains(err.Error(), "perform request:") {
		t.Fatalf("error = %v, want perform request error", err)
	}
}
