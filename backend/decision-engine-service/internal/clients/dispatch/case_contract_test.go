package dispatch

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCaseWorkflowUsesDedicatedAuthenticatedRoute(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer case-token" {
			t.Error("missing case credential")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	client := NewHTTPClient(time.Second, "disabled", "", server.URL, "", "", "").WithCaseAuthToken("case-token")
	for _, kind := range []workflow.ActionType{workflow.ActionTypeCreateCase, workflow.ActionTypeAddToCase, workflow.ActionTypeAddToCaseIfPossible} {
		item := workflow.Execution{ID: "11111111-1111-4111-8111-111111111111", TenantID: "22222222-2222-4222-8222-222222222222", DecisionID: "33333333-3333-4333-8333-333333333333", ActionType: kind, ActionConfig: json.RawMessage(`{"inbox_id":"44444444-4444-4444-8444-444444444444","name":"Alert"}`)}
		if err := client.DispatchWorkflowExecution(context.Background(), item); err != nil {
			t.Fatal(err)
		}
		if received["workflow_execution_id"] != item.ID || received["action_type"] != string(kind) {
			t.Fatalf("contract mismatch: %+v", received)
		}
	}
}

func TestNonCaseActionDoesNotReceiveInternalCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("internal credential leaked")
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	client := NewHTTPClient(time.Second, "token", "internal-secret", "http://invalid-case-service", "", "", "")
	raw, _ := json.Marshal(map[string]string{"url": server.URL})
	if err := client.DispatchWorkflowExecution(context.Background(), workflow.Execution{ActionType: workflow.ActionTypeEmitEvent, ActionConfig: raw}); err != nil {
		t.Fatal(err)
	}
	if err := client.DispatchWorkflowExecution(context.Background(), workflow.Execution{ActionType: workflow.ActionTypeAddTag, ActionConfig: json.RawMessage(`{"tag":"x"}`)}); err == nil {
		t.Fatal("non-case action fell through to case service")
	}
}
