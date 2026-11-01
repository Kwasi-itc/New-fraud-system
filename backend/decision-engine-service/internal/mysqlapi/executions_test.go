package mysqlapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

func TestCancellationRequiresExplicitBoundedPrecondition(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `[]`, `{"expected_generation":null}`, `{"expected_generation":-1}`, `{"expected_generation":2147483648}`, `{"expected_generation":0.5}`, `{"expected_generation":"0"}`, `{"expected_generation":0,"tenant_id":"forged"}`, `{"expected_generation":0,"expected_generation":1}`, `{"expected_generation":0} {}`, strings.Repeat(" ", 1025)} {
		if _, err := DecodeCancellation(strings.NewReader(body)); !errors.Is(err, ErrCancellationRequest) {
			t.Fatalf("accepted %q: %v", body, err)
		}
	}
	for _, body := range []string{`{"expected_generation":0}`, `{"expected_generation":2147483647}`} {
		r, err := DecodeCancellation(strings.NewReader(body))
		if err != nil || r.ExpectedGeneration == nil {
			t.Fatal(r, err)
		}
	}
	var a *ExecutionAdapter
	generation := 0
	if _, err := a.CancelAsync(context.Background(), "tenant", "invalid", CancellationRequest{&generation}); !errors.Is(err, mysql.ErrTenantScope) {
		t.Fatal(err)
	}
	if _, err := a.GetAsync(context.Background(), "tenant", "id"); !errors.Is(err, mysql.ErrDecisionAuthorizationUnavailable) {
		t.Fatal("nil adapter reported success", err)
	}
}
func TestTargetResponsesRetainGenerationCancellationAndReplay(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	record := mysql.AsyncExecutionRecord{AsyncDecisionExecution: execution.AsyncDecisionExecution{ID: "execution", TenantID: "tenant", Status: mysql.ExecutionStatusCancelled, ResultBody: []byte(`{"kept":true}`), CallbackStatus: "cancelled"}, Generation: 4, CallbackMaxAttempts: 5, CancelledAt: &at, CallbackCancelledAt: &at}
	out := AdaptAsyncCreation(mysql.AsyncCreation{Record: record, Replay: true})
	if !out.Replay || out.CompletedInline || out.AsyncDecisionExecution.Generation != 4 || out.AsyncDecisionExecution.CallbackMaxAttempts != 5 {
		t.Fatal(out)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"generation":4`, `"status":"cancelled"`, `"callback_status":"cancelled"`, `"cancelled_at":`, `"callback_cancelled_at":`, `"result_body":{"kept":true}`, `"replay":true`, `"completed_inline":false`} {
		if !strings.Contains(string(raw), part) {
			t.Fatal("lost target/source field", part, string(raw))
		}
	}
	changed := out.AsyncDecisionExecution.CancelledAt.Add(time.Hour)
	*out.AsyncDecisionExecution.CancelledAt = changed
	if record.CancelledAt.Equal(changed) {
		t.Fatal("response aliases owner cancellation timestamp")
	}
	scheduled := AdaptScheduledCreation(mysql.ScheduledCreation{Record: mysql.ScheduledExecutionRecord{ScheduledExecution: execution.ScheduledExecution{Status: mysql.ExecutionStatusCancelled}, Generation: 2, CancelledAt: &at}, Replay: true})
	if scheduled.ScheduledExecution.Generation != 2 || scheduled.ScheduledExecution.Status != "cancelled" || !scheduled.Replay {
		t.Fatal(scheduled)
	}
}
