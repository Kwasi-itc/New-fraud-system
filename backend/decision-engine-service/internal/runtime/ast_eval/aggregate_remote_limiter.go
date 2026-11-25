package ast_eval

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrAggregateOverloaded = errors.New("aggregate_overloaded")

var aggregateRemoteLimiter struct {
	mu    sync.Mutex
	limit int
	slots chan struct{}
}

func acquireAggregateRemoteSlot(ctx context.Context, limit int, maxWait time.Duration) (func(), error) {
	if limit <= 0 {
		setAggregateRemoteConcurrencyLimit(0)
		return func() {}, nil
	}

	aggregateRemoteLimiter.mu.Lock()
	if aggregateRemoteLimiter.limit != limit || aggregateRemoteLimiter.slots == nil {
		aggregateRemoteLimiter.limit = limit
		aggregateRemoteLimiter.slots = make(chan struct{}, limit)
	}
	slots := aggregateRemoteLimiter.slots
	aggregateRemoteLimiter.mu.Unlock()

	setAggregateRemoteConcurrencyLimit(limit)
	waitStartedAt := time.Now()
	var waitCh <-chan time.Time
	var timer *time.Timer
	if maxWait > 0 {
		timer = time.NewTimer(maxWait)
		defer timer.Stop()
		waitCh = timer.C
	}

	select {
	case slots <- struct{}{}:
		recordAggregateRemoteLimiterWait(time.Since(waitStartedAt))
		return func() { <-slots }, nil
	case <-waitCh:
		recordAggregateRemoteLimiterWait(time.Since(waitStartedAt))
		RecordAggregateOverloaded()
		return nil, ErrAggregateOverloaded
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}
