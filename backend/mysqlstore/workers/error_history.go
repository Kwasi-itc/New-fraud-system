package workers

import "errors"

var ErrErrorHistory = errors.New("delivery error history unknown or invalid; explicit translation required")

// ValidateErrorHistory keeps transport claim attempts separate from error count.
// Snoozes/claims are not evidence of errors, and unknown legacy history must not
// be filled from Attempt. Recording a new failure requires room below Attempt.
// This validation neither increments counters nor authorizes replay/migration.
func ValidateErrorHistory(attempt int, count *int64, recordingFailure bool) error {
	if attempt < 0 || attempt > 2147483647 || count == nil || *count < 0 || *count > int64(attempt) || (recordingFailure && *count >= int64(attempt)) {
		return ErrErrorHistory
	}
	return nil
}
