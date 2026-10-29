package workers

import (
	"errors"
	"testing"
	"time"
)

func TestDeliveryOutcomePolicy(t *testing.T) {
	for _, test := range []struct {
		attempt, max int
		retry        bool
		state        DeliveryState
		retention    time.Duration
		release      bool
	}{{1, 3, false, Completed, 24 * time.Hour, false}, {1, 3, true, Available, 0, false}, {3, 3, true, Discarded, 7 * 24 * time.Hour, true}, {4, 3, false, Completed, 24 * time.Hour, false}, {4, 3, true, Discarded, 7 * 24 * time.Hour, true}} {
		got, err := ResolveDeliveryOutcome(test.attempt, test.max, test.retry)
		if err != nil || got.State != test.state || got.Retention != test.retention || got.ReleaseReservation != test.release {
			t.Fatalf("outcome: %+v %v", got, err)
		}
	}
	for _, limits := range [][2]int{{0, 3}, {1, 0}, {-1, 3}} {
		if _, err := ResolveDeliveryOutcome(limits[0], limits[1], true); err == nil {
			t.Fatal("invalid attempt history accepted")
		}
	}
	for _, state := range []DeliveryState{Available, Running, Cancelled} {
		outcome, already, err := ResolveCancellation(state)
		if err != nil || outcome.State != Cancelled || !outcome.ReleaseReservation || already != (state == Cancelled) {
			t.Fatalf("cancel: %+v %v %v", outcome, already, err)
		}
	}
	for _, state := range []DeliveryState{Completed, Discarded} {
		if _, _, err := ResolveCancellation(state); !errors.Is(err, ErrTerminalDelivery) {
			t.Fatal("terminal delivery overwritten")
		}
	}
	if _, _, err := ResolveCancellation("unknown"); err == nil {
		t.Fatal("unknown lifecycle state ignored")
	}
}
