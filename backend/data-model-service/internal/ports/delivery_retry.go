package ports

import "time"

// DeliveryRetryPolicy schedules an execution error from the number of earlier
// recorded errors, not lease claims or domain attempts. It must be local, fast,
// concurrency-safe and free of network/database effects: it runs under the
// delivery transaction lock. Implementations return microsecond precision.
type DeliveryRetryPolicy interface {
	Delay(priorErrors int) (time.Duration, error)
}
