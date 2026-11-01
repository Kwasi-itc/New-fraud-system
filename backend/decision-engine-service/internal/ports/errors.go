package ports

import "errors"

// ErrRiskSnapshotNotFound is a confirmed absence, not invalid input, transport
// failure or invalid storage. Evaluators may treat only this outcome as no risk.
var ErrRiskSnapshotNotFound = errors.New("risk snapshot not found")

var ErrAggregateCapability = errors.New("aggregate operation or value is not supported by the published model")
var ErrPublishedModelUnavailable = errors.New("published tenant model is unavailable, stale or inconsistent")

// LocalTenantReadError distinguishes a direct SQL/storage failure from an
// upstream HTTP dependency. Wrapping during evaluation must retain this type.
type LocalTenantReadError struct{ Cause error }

func (e *LocalTenantReadError) Error() string { return "local tenant-data read failed" }
func (e *LocalTenantReadError) Unwrap() error { return e.Cause }
