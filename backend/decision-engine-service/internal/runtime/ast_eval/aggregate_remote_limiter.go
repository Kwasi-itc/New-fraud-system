package ast_eval

import (
	"context"
	"sync"
	"time"
)

var aggregateRemoteLimiter struct {
	mu    sync.Mutex
	limit int
	slots chan struct{}
}

func acquireAggregateRemoteSlot(ctx context.Context, limit int) (func(), error) {
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
	select {
	case slots <- struct{}{}:
		recordAggregateRemoteLimiterWait(time.Since(waitStartedAt))
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}
