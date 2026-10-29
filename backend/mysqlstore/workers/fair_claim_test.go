package workers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFairClaimer(t *testing.T) {
	var calls [3]atomic.Int64
	queues := []NamedClaim{}
	for i := range calls {
		queues = append(queues, NamedClaim{Queue: string(rune('a' + i)), Claim: func(context.Context) (*DeliveryTask, error) { calls[i].Add(1); return &DeliveryTask{}, nil }})
	}
	f, err := NewFairClaimer(queues)
	if err != nil {
		t.Fatal(err)
	}
	queues[0].Claim = nil // Caller cannot mutate the configured dispatch slice.
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Claim(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, count := range []int64{calls[0].Load(), calls[1].Load(), calls[2].Load()} {
		if count != 10 {
			t.Fatalf("hot queue starved a peer: %d", count)
		}
	}
	failure := errors.New("claim commit uncertain")
	later := false
	f, _ = NewFairClaimer([]NamedClaim{{"first", func(context.Context) (*DeliveryTask, error) { return nil, failure }}, {"second", func(context.Context) (*DeliveryTask, error) { later = true; return nil, nil }}})
	if _, err := f.Claim(context.Background()); !errors.Is(err, failure) || later {
		t.Fatal("claim failure was skipped")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Claim(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, queues := range [][]NamedClaim{nil, {{"q", nil}}, {{"q", func(context.Context) (*DeliveryTask, error) { return nil, nil }}, {"q", func(context.Context) (*DeliveryTask, error) { return nil, nil }}}} {
		if _, err := NewFairClaimer(queues); err == nil {
			t.Fatal("invalid queue plan accepted")
		}
	}
}
