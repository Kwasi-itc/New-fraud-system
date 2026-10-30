package dispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

func TestScreeningDispatchDoesNotFallbackToGeneralCredential(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	defer server.Close()
	general := strings.Repeat("g", 32)
	for _, secret := range []string{"", "short", general} {
		client := NewHTTPClient(time.Second, "token", general, "", server.URL, "", "").WithScreeningAuthToken(secret)
		if err := client.SendScreeningExecution(context.Background(), screening.Execution{}); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid credentials made outbound calls")
	}
}

func TestScreeningDispatchErrorsAreSafeForRiver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "private provider response", http.StatusServiceUnavailable)
	}))
	client := NewHTTPClient(time.Second, "token", "private-token", "", server.URL, "", "").WithScreeningAuthToken("synthetic-intake-private-token-32bytes")
	item := screening.Execution{TenantID: "synthetic", RequestJSON: json.RawMessage(`{}`)}
	for _, networkFailure := range []bool{false, true} {
		if networkFailure {
			server.Close()
		}
		err := client.SendScreeningExecution(context.Background(), item)
		if err == nil || err.Error() != "screening dispatch failed" {
			t.Fatalf("unsafe or missing dispatch error: %v", err)
		}
	}
}

func TestHTTPClientDispatchesJSONRequests(t *testing.T) {
	t.Parallel()

	hits := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/tenants/tenant-1/decision-screenings" && r.Header.Get("Authorization") != "Bearer synthetic-intake-private-token-32bytes" {
			t.Fatalf("authorization header = %q, want Bearer token-1", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/internal/v1/tenants/tenant-1/decision-screenings" && r.Header.Get("Authorization") == "Bearer synthetic-intake-private-token-32bytes" {
			t.Fatal("intake credential disclosed to another integration")
		}
		hits <- r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewHTTPClient(time.Second, "token", "token-1", server.URL+"/workflow", server.URL, server.URL+"/scoring", server.URL+"/outbox").WithScreeningAuthToken("synthetic-intake-private-token-32bytes")
	ctx := context.Background()
	workflowConfig, _ := json.Marshal(map[string]string{"url": server.URL + "/workflow"})

	if err := client.DispatchWorkflowExecution(ctx, workflow.Execution{
		ID:           "wf-1",
		TenantID:     "tenant-1",
		WorkflowID:   stringPtr("workflow-1"),
		DecisionID:   "decision-1",
		ScenarioID:   "scenario-1",
		ActionType:   workflow.ActionTypeEmitEvent,
		ActionConfig: workflowConfig,
	}); err != nil {
		t.Fatalf("DispatchWorkflowExecution() error = %v", err)
	}

	screeningPayload, _ := json.Marshal(map[string]any{"screening": true})
	if err := client.SendScreeningExecution(ctx, screening.Execution{TenantID: "tenant-1", RequestJSON: screeningPayload}); err != nil {
		t.Fatalf("SendScreeningExecution() error = %v", err)
	}

	scoringPayload, _ := json.Marshal(map[string]any{"score": true})
	if err := client.SendScoringRequest(ctx, scoring.Request{RequestJSON: scoringPayload}); err != nil {
		t.Fatalf("SendScoringRequest() error = %v", err)
	}

	outboxPayload, _ := json.Marshal(map[string]any{"event": true})
	if err := client.PublishOutboxEvent(ctx, integration.OutboxEvent{
		ID:      "evt-1",
		Payload: outboxPayload,
	}); err != nil {
		t.Fatalf("PublishOutboxEvent() error = %v", err)
	}

	want := map[string]bool{
		"/workflow": false,
		"/internal/v1/tenants/tenant-1/decision-screenings": false,
		"/scoring": false,
		"/outbox":  false,
	}
	for i := 0; i < 4; i++ {
		path := <-hits
		want[path] = true
	}
	for path, seen := range want {
		if !seen {
			t.Fatalf("expected request to %s", path)
		}
	}
}

func stringPtr(value string) *string {
	return &value
}
