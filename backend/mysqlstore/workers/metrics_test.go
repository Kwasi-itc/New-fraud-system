package workers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
)

func TestDeliveryMetrics(t *testing.T) {
	m := &DeliveryMetrics{}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := m.run(context.Background(), supervisor(t), &DeliveryTask{Heartbeat: func(context.Context) error { return nil }, Execute: func(context.Context) error { _ = m.Snapshot(); return nil }})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	failure := errors.New("lost lease")
	if err := m.run(context.Background(), supervisor(t), &DeliveryTask{Heartbeat: func(context.Context) error { return failure }, Execute: func(context.Context) error { t.Error("stale work started"); return nil }}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if s.HostRuns != 21 || s.HostRunsSucceeded != 20 || s.HostRunsFailed != 1 || s.InFlight != 0 || s.Heartbeats != 21 || s.HeartbeatFailures != 1 {
		t.Fatalf("metrics: %+v", s)
	}
}

func TestDeliveryPollerMetricsPreserveClaimFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &DeliveryMetrics{}
		p, err := poller(t, 1).WithMetrics(m)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		failure := errors.New("claim commit unknown")
		err = p.Run(context.Background(), func(context.Context) (*DeliveryTask, error) {
			calls++
			if calls == 1 {
				return nil, nil
			}
			return nil, failure
		}, func(context.Context, error) error {
			t.Error("claim ambiguity must stop, not report and poll again")
			return nil
		})
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		s := m.Snapshot()
		if s.ClaimCalls != 2 || s.EmptyPolls != 1 || s.ClaimFailures != 1 || s.HostRuns != 0 {
			t.Fatalf("claim metrics: %+v", s)
		}
	})
}
