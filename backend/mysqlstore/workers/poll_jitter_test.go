package workers

import (
	"math"
	"testing"
	"time"
)

func TestPollJitter(t *testing.T) {
	p := poller(t, 1)
	jittered, err := p.WithPollJitter(time.Second / 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.pollJitter != 0 || p.nextDelay() != time.Second {
		t.Fatal("configuration mutated the original poller")
	}
	for range 1000 {
		delay := jittered.nextDelay()
		if delay < time.Second/2 || delay > 1500*time.Millisecond {
			t.Fatalf("jitter outside budget: %v", delay)
		}
	}
	for _, duration := range []time.Duration{-1, time.Second} {
		if _, err := p.WithPollJitter(duration); err == nil {
			t.Fatal("invalid jitter accepted")
		}
	}
	large := *p
	large.pollInterval = time.Duration(math.MaxInt64)
	if _, err := large.WithPollJitter(time.Second); err == nil {
		t.Fatal("overflowing poll delay accepted")
	}
}
