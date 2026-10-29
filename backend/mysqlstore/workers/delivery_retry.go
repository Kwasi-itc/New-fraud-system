package workers

import (
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

// RiverDeliveryRetryPolicy preserves the pinned River v0.31.0 error-count
// schedule: (priorErrors+1)^4 seconds with +/-10% jitter, saturating at the
// duration limit. MySQL storage rounds down by less than one microsecond.
type RiverDeliveryRetryPolicy struct{}

func (RiverDeliveryRetryPolicy) Delay(priorErrors int) (time.Duration, error) {
	return riverDeliveryRetryDelay(priorErrors, rand.Float64())
}

func riverDeliveryRetryDelay(priorErrors int, random float64) (time.Duration, error) {
	if priorErrors < 0 || priorErrors >= math.MaxInt32 || math.IsNaN(random) || random < 0 || random >= 1 {
		return 0, errors.New("bounded error count and random value in [0,1) required")
	}
	maximum := float64(math.MaxInt64) / float64(time.Second)
	seconds := math.Pow(float64(priorErrors)+1, 4)
	if seconds < maximum {
		seconds += seconds * (random*0.2 - 0.1)
	}
	seconds = math.Min(seconds, maximum)
	micros := int64(seconds * 1e6)
	if micros > math.MaxInt64/int64(time.Microsecond) {
		micros = math.MaxInt64 / int64(time.Microsecond)
	}
	return time.Duration(micros) * time.Microsecond, nil
}
