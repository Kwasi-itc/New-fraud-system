package service

import "errors"

var (
	ErrRecordConflict  = errors.New("record uniqueness conflict")
	ErrForbidden       = errors.New("operation is not authorized")
	ErrRequestLimits   = errors.New("request exceeds configured limits")
	ErrUnavailable     = errors.New("required storage capability unavailable")
	ErrCommitUnknown   = errors.New("commit outcome unknown; reconcile the same request identity")
	ErrResourceMissing = errors.New("resource not found")
	ErrBadInput        = errors.New("invalid bounded request")
)

// BoundaryError keeps the public text stable and sanitized while retaining the
// storage cause for internal classification. Transport never exposes SQL text.
type BoundaryError struct{ Kind, Cause error }

func (e *BoundaryError) Error() string   { return e.Kind.Error() }
func (e *BoundaryError) Unwrap() []error { return []error{e.Kind, e.Cause} }
