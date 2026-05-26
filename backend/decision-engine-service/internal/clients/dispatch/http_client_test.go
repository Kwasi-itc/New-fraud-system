package dispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

func TestHTTPClientDispatchesJSONRequests(t *testing.T) {
	t.Parallel()

	hits := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewHTTPClient(time.Second, server.URL+"/workflow", server.URL+"/screening", server.URL+"/scoring", server.URL+"/outbox")
	ctx := context.Background()

	if err := client.DispatchWorkflowExecution(ctx, workflow.Execution{
		ID:         "wf-1",
		TenantID:   "tenant-1",
		WorkflowID: stringPtr("workflow-1"),
		DecisionID: "decision-1",
		ScenarioID: "scenario-1",
		ActionType: workflow.ActionTypeEmitEvent,
	}); err != nil {
		t.Fatalf("DispatchWorkflowExecution() error = %v", err)
	}

	screeningPayload, _ := json.Marshal(map[string]any{"screening": true})
	if err := client.SendScreeningExecution(ctx, screening.Execution{RequestJSON: screeningPayload}); err != nil {
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
		"/workflow":  false,
		"/screening": false,
		"/scoring":   false,
		"/outbox":    false,
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
