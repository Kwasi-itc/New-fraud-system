package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DeliveryTask binds callbacks to one committed claim. Execute owns all durable
// outcome transitions. Neither the poller nor supervisor may infer an outcome
// from an error and issue a second acknowledgement or retry.
type DeliveryTask struct {
	Heartbeat func(context.Context) error
	Execute   func(context.Context) error
}

type DeliveryPoller struct {
	concurrency  int
	pollInterval time.Duration
	claimTimeout time.Duration
	supervisor   *LeaseSupervisor
}

func NewDeliveryPoller(concurrency int, pollInterval, claimTimeout time.Duration, supervisor *LeaseSupervisor) (*DeliveryPoller, error) {
	if concurrency < 1 || concurrency > 1024 || pollInterval <= 0 || claimTimeout <= 0 || supervisor == nil || supervisor.interval <= 0 {
		return nil, errors.New("bounded concurrency, positive polling/claim durations and configured supervisor required")
	}
	return &DeliveryPoller{concurrency, pollInterval, claimTimeout, supervisor}, nil
}

// Run uses fixed worker slots, with no claim-ahead buffer. Claim returns nil when
// no due work exists. Claim errors are fatal (including ambiguous commit), cancel
// all slots and propagate after all executors exit. Execute errors are passed to
// report; its failure is fatal. All callbacks must honor context and support
// concurrent calls. report must durably/observably record failures, not discard
// them. Polling after an execution error is delayed to avoid a failure hot loop.
// External cancellation is returned as an error, never successful completion.
func (p *DeliveryPoller) Run(ctx context.Context, claim func(context.Context) (*DeliveryTask, error), report func(context.Context, error) error) error {
	if p == nil || p.concurrency < 1 || p.pollInterval <= 0 || p.claimTimeout <= 0 || p.supervisor == nil || claim == nil || report == nil {
		return errors.New("configured delivery poller, claim and error reporter required")
	}
	run, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	failures := make(chan error, p.concurrency)
	var workers sync.WaitGroup
	for range p.concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := p.poll(run, claim, report); err != nil {
				failures <- err
				cancel(err)
			}
		}()
	}
	workers.Wait()
	close(failures)
	var result error
	for err := range failures {
		result = errors.Join(result, err)
	}
	return errors.Join(result, context.Cause(run))
}

func (p *DeliveryPoller) poll(ctx context.Context, claim func(context.Context) (*DeliveryTask, error), report func(context.Context, error) error) error {
	for ctx.Err() == nil {
		call, cancel := context.WithTimeout(ctx, p.claimTimeout)
		task, err := claim(call)
		if err == nil {
			err = call.Err()
		}
		cancel()
		if err != nil {
			return fmt.Errorf("claim delivery: %w", err)
		}
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if task != nil {
			if task.Execute == nil || task.Heartbeat == nil {
				return errors.New("claimed delivery is missing execution or heartbeat")
			}
			err = p.supervisor.Run(ctx, task.Heartbeat, task.Execute)
			if err == nil {
				continue
			}
			if ctx.Err() != nil {
				return errors.Join(err, context.Cause(ctx))
			}
			if reportErr := report(ctx, err); reportErr != nil {
				return errors.Join(err, fmt.Errorf("report delivery failure: %w", reportErr))
			}
		}
		timer := time.NewTimer(p.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}
	return context.Cause(ctx)
}
