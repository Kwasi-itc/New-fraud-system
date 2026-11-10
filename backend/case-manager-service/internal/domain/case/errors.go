package casepkg

import (
	"errors"
	"fmt"
)

var (
	ErrValidation  = errors.New("invalid request")
	ErrNotFound    = errors.New("not found")
	ErrForbidden   = errors.New("forbidden")
	ErrConflict    = errors.New("conflict")
	ErrUnavailable = errors.New("required owner contract unavailable")
)

func Invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrValidation, message)
}

func ValidStatus(s Status) bool {
	return s == StatusPending || s == StatusInvestigating || s == StatusClosed
}

func ValidOutcome(o Outcome) bool {
	return o == OutcomeUnset || o == OutcomeFalsePositive || o == OutcomeValuableAlert || o == OutcomeConfirmedRisk
}

func ValidType(t Type) bool {
	return t == TypeDecision || t == TypeContinuousScreening || t == TypeManualScreening
}
