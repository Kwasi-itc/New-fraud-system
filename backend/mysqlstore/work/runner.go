package work

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

type RunnerConfig struct {
	Metrics                                   *RunnerMetrics
	Queue                                     string
	Concurrency                               int
	Lease, Timeout, Poll, RetryBase, RetryMax time.Duration
}
type Handler func(context.Context, Lease) error

// Run owns bounded claims, heartbeats, expiry rescue and joined cancellation.
// Handlers must fence business effects and complete work in that transaction.
// The error observer is mandatory; transient DB and handler failures are visible.
// Queue time uses the database; client clocks only schedule polling/heartbeats.
func (q *Queue) Run(ctx context.Context, c RunnerConfig, handle Handler, observe func(error)) error {
	if q == nil || !ValidQueueName(c.Queue) || c.Concurrency < 1 || c.Concurrency > 64 || c.Lease < 3*time.Second || c.Lease > 30*time.Minute || c.Timeout < time.Second || c.Timeout > 30*time.Minute || c.Poll < 10*time.Millisecond || c.Poll > time.Minute || c.RetryBase < time.Second || c.RetryMax < c.RetryBase || c.RetryMax > 24*time.Hour || handle == nil || observe == nil {
		return errors.New("complete bounded worker configuration required")
	}
	// Each runner rescues only its configured queue; retain any tenant scope.
	local := *q
	local.rescueQueue = c.Queue
	q = &local
	var wg sync.WaitGroup
	wait := func(d time.Duration) bool {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		}
	}
	for i := 0; i < c.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				claimStart := time.Now()
				leases, err := q.Claim(ctx, c.Queue, 1, c.Lease)
				claimElapsed := time.Since(claimStart)
				c.Metrics.update(func(m *RunnerCounters) {
					m.ClaimCalls++
					m.ClaimDuration.add(claimElapsed)
					if err != nil {
						m.ClaimFailures++
					} else {
						m.Claims += uint64(len(leases))
						for _, l := range leases {
							m.QueueWait.add(l.QueueWait)
						}
					}
				})
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					observe(err)
				}
				for _, l := range leases {
					err := q.process(ctx, c, l, handle)
					c.Metrics.update(func(m *RunnerCounters) {
						m.ProcessesFinished++
						if err != nil {
							m.ProcessFailures++
						}
					})
					if err != nil && ctx.Err() == nil {
						observe(err)
					}
				}
				if !wait(c.Poll + time.Duration(rand.Int64N(int64(c.Poll/2)+1))) {
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			n, err := q.Reap(ctx, 100, c.RetryBase)
			c.Metrics.update(func(m *RunnerCounters) {
				m.ReapCalls++
				if err != nil {
					m.ReapFailures++
				} else {
					m.ExpiredConfirmed += uint64(n)
				}
			})
			if err != nil && ctx.Err() == nil {
				observe(err)
			}
			if !wait(c.Lease / 2) {
				return
			}
		}
	}()
	wg.Wait()
	return ctx.Err()
}

func (q *Queue) process(parent context.Context, c RunnerConfig, l Lease, handle Handler) error {
	ctx, cancel := context.WithTimeout(parent, c.Timeout)
	done := make(chan struct{})
	heartbeat := make(chan error, 1)
	go func() {
		defer close(done)
		timer := time.NewTicker(c.Lease / 3)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				err := q.Heartbeat(ctx, l, c.Lease)
				c.Metrics.update(func(m *RunnerCounters) {
					m.HeartbeatCalls++
					if err != nil {
						m.HeartbeatFailures++
					}
				})
				if err != nil {
					heartbeat <- err
					cancel()
					return
				}
			}
		}
	}()
	c.Metrics.update(func(m *RunnerCounters) { m.HandlerCalls++; m.ActiveHandlers++ })
	started := time.Now()
	err := handle(ctx, l)
	handlerElapsed := time.Since(started)
	c.Metrics.update(func(m *RunnerCounters) {
		m.ActiveHandlers--
		m.HandlerDuration.add(handlerElapsed)
		if err != nil {
			m.HandlerFailures++
		}
	})
	cancel()
	<-done
	select {
	case e := <-heartbeat:
		if err != nil {
			err = errors.Join(err, e)
		}
	default:
	}
	if err == nil {
		var state string
		if checkErr := q.db.QueryRowContext(parent, "SELECT state FROM "+q.jobs+" WHERE tenant_id=? AND id=? AND attempt=?", mysqlstore.UUID(l.TenantID), mysqlstore.UUID(l.ID), l.Attempt).Scan(&state); checkErr != nil {
			err = checkErr
		} else if state == "completed" || state == "discarded" || state == "cancelled" {
			return nil
		} else {
			err = errors.New("handler returned without committing terminal work state")
		}
	}
	if parent.Err() != nil {
		return parent.Err()
	} // Expiry recovery handles cancellation.
	delay := c.RetryBase
	for i := 1; i < l.Attempt && delay < c.RetryMax; i++ {
		if delay > c.RetryMax/2 {
			delay = c.RetryMax
		} else {
			delay *= 2
		}
	}
	if delay > c.RetryMax {
		delay = c.RetryMax
	}
	// Bounded jitter; persist only a fixed redacted code, never payload/error text.
	if room := c.RetryMax - delay; room > 0 {
		jitter := delay / 4
		if jitter > room {
			jitter = room
		}
		delay += time.Duration(rand.Int64N(int64(jitter) + 1))
	}
	retryErr := q.Retry(parent, l, delay, "handler_failed")
	c.Metrics.update(func(m *RunnerCounters) {
		m.RetryCalls++
		switch {
		case errors.Is(retryErr, ErrLease):
			m.RetryLeaseRejections++
		case retryErr != nil:
			m.RetryFailures++
		case l.Attempt >= l.MaxAttempts:
			m.ExhaustionsConfirmed++
		default:
			m.RetriesConfirmed++
		}
	})
	if retryErr != nil && !errors.Is(retryErr, ErrLease) {
		return errors.Join(err, retryErr)
	}
	return err
}
