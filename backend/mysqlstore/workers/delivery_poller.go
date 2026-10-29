package workers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
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
	pollJitter   time.Duration
	metrics      *DeliveryMetrics
}

func NewDeliveryPoller(concurrency int, pollInterval, claimTimeout time.Duration, supervisor *LeaseSupervisor) (*DeliveryPoller, error) {
	if concurrency < 1 || concurrency > 1024 || pollInterval <= 0 || claimTimeout <= 0 || supervisor == nil || supervisor.interval <= 0 {
		return nil, errors.New("bounded concurrency, positive polling/claim durations and configured supervisor required")
	}
	return &DeliveryPoller{concurrency: concurrency, pollInterval: pollInterval, claimTimeout: claimTimeout, supervisor: supervisor}, nil
}

// WithPollJitter returns an immutable configured copy. Jitter is explicit (zero
// retains the existing exact interval), bounded to half the interval and cannot
// overflow or produce a zero/negative delay. Configure before starting Run.
func (p *DeliveryPoller) WithPollJitter(jitter time.Duration) (*DeliveryPoller, error) {
	if p == nil || p.pollInterval <= 0 || p.supervisor == nil || jitter < 0 || jitter > p.pollInterval/2 || p.pollInterval > time.Duration(math.MaxInt64)-jitter {
		return nil, errors.New("configured poller and bounded nonnegative poll jitter required")
	}
	copy := *p
	copy.pollJitter = jitter
	return &copy, nil
}

func (p *DeliveryPoller) nextDelay() time.Duration {
	if p.pollJitter == 0 {
		return p.pollInterval
	}
	return p.pollInterval - p.pollJitter + time.Duration(rand.Int64N(2*int64(p.pollJitter)+1))
}

func (p *DeliveryPoller) WithMetrics(metrics *DeliveryMetrics) (*DeliveryPoller, error) {
	if p == nil || p.supervisor == nil || p.pollInterval <= 0 || metrics == nil {
		return nil, errors.New("configured poller and delivery metrics required")
	}
	copy := *p
	copy.metrics = metrics
	return &copy, nil
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
		started := time.Now()
		if p.metrics != nil {
			p.metrics.claims.Add(1)
		}
		task, err := claim(call)
		if err == nil {
			err = call.Err()
		}
		cancel()
		if p.metrics != nil {
			p.metrics.claimMicros.Add(time.Since(started).Microseconds())
			if err != nil {
				p.metrics.claimFailures.Add(1)
			} else if task == nil {
				p.metrics.empty.Add(1)
			}
		}
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
			if p.metrics == nil {
				err = p.supervisor.Run(ctx, task.Heartbeat, task.Execute)
			} else {
				err = p.metrics.run(ctx, p.supervisor, task)
			}
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
		timer := time.NewTimer(p.nextDelay())
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}
	return context.Cause(ctx)
}
