package mysqlapi

import (
	"context"
	"errors"
	"time"
)

type ExecutionWaitPolicy struct{ MaxWindow, PollInterval time.Duration }

func (p ExecutionWaitPolicy) Validate() error {
	if p.MaxWindow <= 0 || p.MaxWindow > time.Minute || p.PollInterval < 10*time.Millisecond || p.PollInterval > p.MaxWindow {
		return errors.New("wait maximum in (0,1m] and poll interval in [10ms,maximum] required")
	}
	return nil
}

// WaitAsync observes COMMITTED states using independently authorized short
// reads. It never owns/executes a job, holds a SQL transaction while sleeping or
// claims inline success merely because the wait budget elapsed.
func (a *ExecutionAdapter) WaitAsync(ctx context.Context, tenant, id string, window time.Duration, policy ExecutionWaitPolicy) (AsyncExecutionResponse, bool, error) {
	if a == nil || a.store == nil {
		return AsyncExecutionResponse{}, false, errors.New("execution read adapter required")
	}
	return waitAsync(ctx, window, policy, func(ctx context.Context) (AsyncExecutionResponse, error) { return a.GetAsync(ctx, tenant, id) })
}
func waitAsync(ctx context.Context, window time.Duration, policy ExecutionWaitPolicy, read func(context.Context) (AsyncExecutionResponse, error)) (AsyncExecutionResponse, bool, error) {
	if err := policy.Validate(); err != nil {
		return AsyncExecutionResponse{}, false, err
	}
	if window < 0 || window > policy.MaxWindow || read == nil {
		return AsyncExecutionResponse{}, false, errors.New("requested wait outside configured maximum")
	}
	if err := ctx.Err(); err != nil {
		return AsyncExecutionResponse{}, false, err
	}
	if window == 0 {
		out, err := read(ctx)
		if err != nil {
			return AsyncExecutionResponse{}, false, err
		}
		return out, out.Status == "completed", nil
	}
	wait, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	var last AsyncExecutionResponse
	observed := false
	for {
		out, err := read(wait)
		if ctx.Err() != nil {
			return AsyncExecutionResponse{}, false, context.Cause(ctx)
		}
		if err != nil {
			if wait.Err() != nil && observed && errors.Is(err, context.DeadlineExceeded) {
				return last, false, nil
			}
			return AsyncExecutionResponse{}, false, err
		}
		last, observed = out, true
		if out.Status == "completed" || out.Status == "failed" || out.Status == "cancelled" {
			return out, out.Status == "completed", nil
		}
		timer := time.NewTimer(policy.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return AsyncExecutionResponse{}, false, context.Cause(ctx)
		case <-wait.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return AsyncExecutionResponse{}, false, context.Cause(ctx)
			}
			return last, false, nil
		case <-timer.C:
		}
	}
}
