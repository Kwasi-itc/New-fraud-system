package mysqlapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func waitState(status string) AsyncExecutionResponse {
	v := AsyncExecutionResponse{}
	v.ID = "execution"
	v.Status = status
	return v
}
func TestExecutionWaitCommittedStatesTimeoutAndErrors(t *testing.T) {
	policy := ExecutionWaitPolicy{MaxWindow: time.Second, PollInterval: 10 * time.Millisecond}
	for _, terminal := range []string{"completed", "failed", "cancelled"} {
		calls := 0
		v, completed, err := waitAsync(context.Background(), time.Second, policy, func(context.Context) (AsyncExecutionResponse, error) {
			calls++
			if calls == 1 {
				return waitState("running"), nil
			}
			return waitState(terminal), nil
		})
		if err != nil || v.Status != terminal || completed != (terminal == "completed") || calls != 2 {
			t.Fatal(v, completed, calls, err)
		}
	}
	calls := 0
	v, completed, err := waitAsync(context.Background(), 25*time.Millisecond, policy, func(context.Context) (AsyncExecutionResponse, error) { calls++; return waitState("queued"), nil })
	if err != nil || completed || v.Status != "queued" || calls < 1 || calls > 4 {
		t.Fatal(v, completed, calls, err)
	}
	denied := errors.New("revoked authority")
	calls = 0
	if _, _, err := waitAsync(context.Background(), time.Second, policy, func(context.Context) (AsyncExecutionResponse, error) {
		calls++
		if calls == 1 {
			return waitState("running"), nil
		}
		return AsyncExecutionResponse{}, denied
	}); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := waitAsync(ctx, time.Second, policy, func(context.Context) (AsyncExecutionResponse, error) {
		t.Fatal("read after cancellation")
		return waitState("completed"), nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestExecutionWaitInputAndDeadlineAreNotSuccess(t *testing.T) {
	p := ExecutionWaitPolicy{MaxWindow: time.Second, PollInterval: 10 * time.Millisecond}
	for _, window := range []time.Duration{-1, 2 * time.Second} {
		if _, _, err := waitAsync(context.Background(), window, p, func(context.Context) (AsyncExecutionResponse, error) { return waitState("completed"), nil }); err == nil {
			t.Fatal("unbounded wait")
		}
	}
	if _, complete, err := waitAsync(context.Background(), 0, p, func(context.Context) (AsyncExecutionResponse, error) { return waitState("running"), nil }); err != nil || complete {
		t.Fatal(complete, err)
	}
	if _, _, err := waitAsync(context.Background(), time.Millisecond, p, func(ctx context.Context) (AsyncExecutionResponse, error) {
		<-ctx.Done()
		return AsyncExecutionResponse{}, ctx.Err()
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("no authorized state must not become success", err)
	}
	for _, raw := range []string{`{}`, `{"expected_generation":null}`, `{"expected_generation":0,"expected_generation":1}`, `{"expected_generation":-1}`, `{"expected_generation":0} {}`} {
		if _, err := DecodeRedrive(strings.NewReader(raw)); err == nil {
			t.Fatal("invalid redrive", raw)
		}
	}
	if r, err := DecodeRedrive(strings.NewReader(`{"expected_generation":0}`)); err != nil || r.ExpectedGeneration == nil || *r.ExpectedGeneration != 0 {
		t.Fatal(r, err)
	}
}
