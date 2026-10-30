package mysql

import (
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// One transport failure (including an expired claim) consumes error history,
// never a domain retry. Receipts remain permanent in the decision-owner model;
// the shared ReleaseReservation flag is intentionally not applied here.
func deliveryFailurePlan(attempt, maximum int, priorErrors int64) (workers.DeliveryState, time.Duration, error) {
	if attempt > maximum {
		return "", 0, errors.New("transport attempt exceeds its immutable maximum")
	}
	if err := workers.ValidateErrorHistory(attempt, &priorErrors, true); err != nil {
		return "", 0, err
	}
	out, err := workers.ResolveDeliveryOutcome(attempt, maximum, true)
	if err != nil {
		return "", 0, err
	}
	if out.State == workers.Discarded {
		return out.State, 0, nil
	}
	delay, err := (workers.RiverDeliveryRetryPolicy{}).Delay(int(priorErrors))
	return out.State, delay, err
}
