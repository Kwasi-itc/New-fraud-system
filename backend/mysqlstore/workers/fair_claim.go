package workers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"
)

var queueLabel = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type NamedClaim struct {
	Queue string
	Claim func(context.Context) (*DeliveryTask, error)
}

// FairClaimer rotates the first queue probed across concurrent worker slots.
// A call probes each queue at most once, stops at the first committed claim and
// never buffers work. Empty queues may be skipped; failed/ambiguous claims may
// not. Owners retain independent queue stores, credentials and effect contracts.
type FairClaimer struct {
	queues []NamedClaim
	next   atomic.Uint64
}

func NewFairClaimer(queues []NamedClaim) (*FairClaimer, error) {
	if len(queues) < 1 || len(queues) > 256 {
		return nil, errors.New("1..256 explicit queue claimers required")
	}
	seen := make(map[string]bool, len(queues))
	for _, q := range queues {
		if !queueLabel.MatchString(q.Queue) || seen[q.Queue] || q.Claim == nil {
			return nil, errors.New("unique queue labels and claim functions required")
		}
		seen[q.Queue] = true
	}
	return &FairClaimer{queues: append([]NamedClaim(nil), queues...)}, nil
}

func (f *FairClaimer) Claim(ctx context.Context) (*DeliveryTask, error) {
	if f == nil || len(f.queues) == 0 {
		return nil, errors.New("configured fair queue claimer required")
	}
	start := (f.next.Add(1) - 1) % uint64(len(f.queues))
	for i := range f.queues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := f.queues[(int(start)+i)%len(f.queues)]
		task, err := q.Claim(ctx)
		if err != nil {
			return nil, fmt.Errorf("claim queue %s: %w", q.Queue, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if task != nil {
			return task, nil
		}
	}
	return nil, nil
}
