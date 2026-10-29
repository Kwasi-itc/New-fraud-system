package workers

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// LeaseSupervisor runs one already-claimed delivery. It never acknowledges,
// retries or releases it: the execution store owns durable outcomes and expiry
// recovers an interrupted attempt. Cancellation is not a physical DDL fence.
type LeaseSupervisor struct {
	interval         time.Duration
	heartbeatTimeout time.Duration
	workTimeout      time.Duration
}

// All durations are explicit. The lease duration must match the queue store;
// allowing at least two heartbeat intervals leaves room for one slow heartbeat.
func NewLeaseSupervisor(lease, interval, heartbeatTimeout, workTimeout time.Duration) (*LeaseSupervisor, error) {
	if lease <= 0 || interval <= 0 || interval > lease/3 || heartbeatTimeout <= 0 || heartbeatTimeout > interval || workTimeout <= 0 {
		return nil, errors.New("positive worker durations and heartbeat timeout <= interval <= lease/3 required")
	}
	return &LeaseSupervisor{interval, heartbeatTimeout, workTimeout}, nil
}

// Run first verifies/renews the lease, then supervises work. Both callbacks must
// honor context cancellation. Run joins the heartbeat before returning and waits
// for work to exit even after shutdown, timeout or heartbeat failure; a caller
// must not reuse its concurrency slot while the previous executor is still live.
// A heartbeat failure racing a committed outcome is returned conservatively;
// recovery must inspect durable state, never blindly rerun or overwrite it.
func (s *LeaseSupervisor) Run(ctx context.Context, heartbeat, work func(context.Context) error) error {
	if s == nil || s.interval <= 0 || s.heartbeatTimeout <= 0 || s.workTimeout <= 0 || heartbeat == nil || work == nil {
		return errors.New("configured lease supervisor and callbacks required")
	}
	deadline, stopDeadline := context.WithTimeout(ctx, s.workTimeout)
	defer stopDeadline()
	execution, cancelExecution := context.WithCancelCause(deadline)
	defer cancelExecution(nil)
	renew := func(ctx context.Context) error {
		call, cancel := context.WithTimeout(ctx, s.heartbeatTimeout)
		defer cancel()
		err := heartbeat(call)
		if err == nil {
			err = call.Err()
		}
		if err != nil {
			return fmt.Errorf("delivery heartbeat: %w", err)
		}
		return nil
	}
	if err := execution.Err(); err != nil {
		return context.Cause(execution)
	}
	if err := renew(execution); err != nil {
		return err
	}
	if execution.Err() != nil {
		return context.Cause(execution)
	}
	heartbeats, stopHeartbeats := context.WithCancel(execution)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeats.Done():
				done <- nil
				return
			case <-ticker.C:
				if heartbeats.Err() != nil {
					continue
				}
				if err := renew(heartbeats); err != nil {
					// Cancelling a heartbeat after work finishes is normal. Other
					// failures, including stale leases, must remain observable.
					if heartbeats.Err() != nil && errors.Is(err, context.Canceled) {
						done <- nil
						return
					}
					cancelExecution(err)
					done <- err
					return
				}
			}
		}
	}()
	// Deferred joining also prevents a leaked renewer if work panics. Panics
	// propagate to the process boundary rather than becoming successful jobs.
	var heartbeatErr error
	defer func() {
		stopHeartbeats()
		if done != nil {
			<-done
		}
	}()
	workErr := work(execution)
	stopHeartbeats()
	heartbeatErr = <-done
	done = nil
	return errors.Join(workErr, heartbeatErr, context.Cause(execution))
}
