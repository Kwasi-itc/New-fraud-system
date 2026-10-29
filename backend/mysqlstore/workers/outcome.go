package workers

import (
	"errors"
	"time"
)

type DeliveryState string

const (
	Available DeliveryState = "available"
	Running   DeliveryState = "running"
	Completed DeliveryState = "completed"
	Cancelled DeliveryState = "cancelled"
	Discarded DeliveryState = "discarded"
)

type DeliveryOutcome struct {
	State              DeliveryState
	Retention          time.Duration
	ReleaseReservation bool
}

// ResolveDeliveryOutcome preserves the existing data-model/River transport
// policy: retained completion deduplicates; exhausted deliveries release their
// reservation and retain operator evidence for seven days. This is a pure
// decision, not an acknowledgement. Owners must lock/revalidate the lease and
// persist local effects, outcome and reservation changes in ONE transaction.
func ResolveDeliveryOutcome(attempt, maxAttempts int, retry bool) (DeliveryOutcome, error) {
	if attempt < 1 || maxAttempts < 1 || attempt > 2147483647 || maxAttempts > 2147483647 {
		return DeliveryOutcome{}, errors.New("positive signed attempt limits required")
	}
	if !retry {
		return DeliveryOutcome{State: Completed, Retention: 24 * time.Hour}, nil
	}
	if attempt >= maxAttempts {
		return DeliveryOutcome{State: Discarded, Retention: 7 * 24 * time.Hour, ReleaseReservation: true}, nil
	}
	return DeliveryOutcome{State: Available}, nil
}

var ErrTerminalDelivery = errors.New("delivery is already terminal")

// ResolveCancellation is idempotent only for an already-cancelled delivery.
// It never rewrites successful or exhausted work, nor accepts unknown states.
func ResolveCancellation(state DeliveryState) (DeliveryOutcome, bool, error) {
	switch state {
	case Cancelled:
		return DeliveryOutcome{State: Cancelled, Retention: 24 * time.Hour, ReleaseReservation: true}, true, nil
	case Available, Running:
		return DeliveryOutcome{State: Cancelled, Retention: 24 * time.Hour, ReleaseReservation: true}, false, nil
	case Completed, Discarded:
		return DeliveryOutcome{}, false, ErrTerminalDelivery
	default:
		return DeliveryOutcome{}, false, errors.New("unsupported delivery state")
	}
}
