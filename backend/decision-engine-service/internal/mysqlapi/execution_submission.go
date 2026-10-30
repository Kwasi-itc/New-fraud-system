package mysqlapi

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

// ExecutionSubmission composes actual admission and read/wait operations without
// registering public routes or manufacturing trusted identity. The caller must
// build both stores with the same validated user/session and tenant authority.
type ExecutionSubmission struct {
	admission *mysqlexecution.Admission
	read      *ExecutionAdapter
	wait      ExecutionWaitPolicy
}

// ExecutionWaitError means admission committed, but waiting/read authorization
// failed or the caller cancelled. The execution must not be submitted again
// with a new identity merely because the HTTP request did not return a result.
type ExecutionWaitError struct {
	ExecutionID string
	Cause       error
}

func (e *ExecutionWaitError) Error() string {
	return "execution admitted; wait failed; reconcile by execution ID"
}
func (e *ExecutionWaitError) Unwrap() error { return e.Cause }

func NewExecutionSubmission(admission *mysqlexecution.Admission, read *ExecutionAdapter, wait ExecutionWaitPolicy) (*ExecutionSubmission, error) {
	if admission == nil || read == nil || !admission.MatchesExecutionStore(read.store) {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	if err := wait.Validate(); err != nil {
		return nil, err
	}
	return &ExecutionSubmission{admission, read, wait}, nil
}
func (a *ExecutionSubmission) CreateScheduled(ctx context.Context, tenant, scenario string, at time.Time, request service.ScheduledExecutionRequest) (ScheduledCreationResponse, error) {
	if a == nil || a.admission == nil {
		return ScheduledCreationResponse{}, mysql.ErrDecisionAuthorizationUnavailable
	}
	v, err := a.admission.CreateScheduled(ctx, tenant, scenario, at, request)
	if err != nil {
		return ScheduledCreationResponse{}, err
	}
	return AdaptScheduledCreation(v), nil
}
func (a *ExecutionSubmission) CreateAsync(ctx context.Context, tenant string, request service.AsyncDecisionExecutionRequest) (AsyncCreationResponse, error) {
	if a == nil || a.admission == nil {
		return AsyncCreationResponse{}, mysql.ErrDecisionAuthorizationUnavailable
	}
	if request.WaitTimeoutMS < 0 || request.WaitTimeoutMS > int(a.wait.MaxWindow/time.Millisecond) {
		return AsyncCreationResponse{}, service.ErrExecutionPreparation
	}
	window := time.Duration(request.WaitTimeoutMS) * time.Millisecond
	v, err := a.admission.CreateAsync(ctx, tenant, request)
	if err != nil {
		return AsyncCreationResponse{}, err
	}
	out, complete, err := a.read.WaitAsync(ctx, tenant, v.Record.ID, window, a.wait)
	if err != nil {
		return AsyncCreationResponse{}, &ExecutionWaitError{ExecutionID: v.Record.ID, Cause: err}
	}
	return AsyncCreationResponse{AsyncDecisionExecution: out, Replay: v.Replay, CompletedInline: complete}, nil
}
