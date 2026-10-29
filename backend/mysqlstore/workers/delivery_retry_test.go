package workers

import (
	"math"
	"testing"
	"time"
)

func TestRiverDeliveryRetryPolicy(t *testing.T) {
	for _, test := range []struct {
		prior   int
		seconds int64
	}{{0, 1}, {1, 16}, {2, 81}, {3, 256}, {24, 390625}} {
		for _, random := range []float64{0, 0.5, 0.999999} {
			got, err := riverDeliveryRetryDelay(test.prior, random)
			want := time.Duration(float64(test.seconds) * float64(time.Second) * (1 + random*0.2 - 0.1)).Truncate(time.Microsecond)
			if err != nil || got != want {
				t.Fatalf("prior=%d jitter=%f got=%s want=%s err=%v", test.prior, random, got, want, err)
			}
		}
	}
	maximum := time.Duration(math.MaxInt64).Truncate(time.Microsecond)
	for _, prior := range []int{309, 310, math.MaxInt32 - 1} {
		for _, random := range []float64{0, 0.5, 0.999999} {
			got, err := riverDeliveryRetryDelay(prior, random)
			if err != nil || got != maximum {
				t.Fatalf("saturation prior=%d got=%s err=%v", prior, got, err)
			}
		}
	}
	for _, prior := range []int{-1, math.MaxInt32} {
		if _, err := riverDeliveryRetryDelay(prior, 0.5); err == nil {
			t.Fatal("invalid count accepted")
		}
	}
	for _, random := range []float64{-1, 1, math.NaN(), math.Inf(1)} {
		if _, err := riverDeliveryRetryDelay(0, random); err == nil {
			t.Fatal("invalid random value accepted")
		}
	}
	for range 100 {
		got, err := (RiverDeliveryRetryPolicy{}).Delay(0)
		if err != nil || got < 900*time.Millisecond || got >= 1100*time.Millisecond || got%time.Microsecond != 0 {
			t.Fatalf("jitter outside contract: %s %v", got, err)
		}
	}
}
