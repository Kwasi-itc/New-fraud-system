// Package mysqlapi contains target-only boundary adapters. It does not register
// routes, choose MySQL at runtime or supply authentication/owner providers.
package mysqlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/google/uuid"
)

// Target responses preserve existing source fields and explicitly expose the
// generation and cancellation state. Never silently drop these via legacy DTOs.
type ScheduledExecutionResponse struct {
	dto.ScheduledExecutionResponse
	Generation  int        `json:"generation"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
}
type AsyncExecutionResponse struct {
	dto.AsyncDecisionExecutionResponse
	Generation          int        `json:"generation"`
	CallbackMaxAttempts int        `json:"callback_max_attempts"`
	CancelledAt         *time.Time `json:"cancelled_at,omitempty"`
	CallbackCancelledAt *time.Time `json:"callback_cancelled_at,omitempty"`
}
type ScheduledCreationResponse struct {
	ScheduledExecution ScheduledExecutionResponse `json:"scheduled_execution"`
	Replay             bool                       `json:"replay"`
}
type AsyncCreationResponse struct {
	AsyncDecisionExecution AsyncExecutionResponse `json:"async_decision_execution"`
	Replay                 bool                   `json:"replay"`
	// This adapter does not evaluate inline or wait on the worker. A future
	// wait adapter must report real committed completion, not fabricate it.
	CompletedInline bool `json:"completed_inline"`
}

func AdaptScheduled(record mysql.ScheduledExecutionRecord) ScheduledExecutionResponse {
	return ScheduledExecutionResponse{dto.AdaptScheduledExecution(record.ScheduledExecution), record.Generation, copyTime(record.CancelledAt)}
}
func AdaptAsync(record mysql.AsyncExecutionRecord) AsyncExecutionResponse {
	return AsyncExecutionResponse{dto.AdaptAsyncDecisionExecution(record.AsyncDecisionExecution), record.Generation, record.CallbackMaxAttempts, copyTime(record.CancelledAt), copyTime(record.CallbackCancelledAt)}
}
func AdaptScheduledCreation(value mysql.ScheduledCreation) ScheduledCreationResponse {
	return ScheduledCreationResponse{AdaptScheduled(value.Record), value.Replay}
}
func AdaptAsyncCreation(value mysql.AsyncCreation) AsyncCreationResponse {
	return AsyncCreationResponse{AsyncDecisionExecution: AdaptAsync(value.Record), Replay: value.Replay}
}
func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

var ErrCancellationRequest = errors.New("cancellation requires an explicit nonnegative signed expected_generation and no other fields")

type CancellationRequest struct {
	// A pointer distinguishes explicitly requested generation zero from an
	// omitted precondition. Tenant/actor/lease claims are not accepted in JSON.
	ExpectedGeneration *int `json:"expected_generation"`
}

func (r CancellationRequest) Validate() error {
	if r.ExpectedGeneration == nil || *r.ExpectedGeneration < 0 || *r.ExpectedGeneration > math.MaxInt32 {
		return ErrCancellationRequest
	}
	return nil
}
func DecodeCancellation(body io.Reader) (CancellationRequest, error) {
	if body == nil {
		return CancellationRequest{}, ErrCancellationRequest
	}
	// This fixed one-field syntax needs no unbounded body allocation. Reading
	// max+1 makes overflow explicit even if a valid document comes first.
	raw, err := io.ReadAll(io.LimitReader(body, 1025))
	if err != nil {
		return CancellationRequest{}, errors.Join(ErrCancellationRequest, err)
	}
	if len(raw) > 1024 {
		return CancellationRequest{}, ErrCancellationRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return CancellationRequest{}, ErrCancellationRequest
	}
	key, err := decoder.Token()
	if err != nil || key != "expected_generation" {
		return CancellationRequest{}, ErrCancellationRequest
	}
	var request CancellationRequest
	if err := decoder.Decode(&request.ExpectedGeneration); err != nil {
		return CancellationRequest{}, ErrCancellationRequest
	}
	// Reject duplicate keys rather than accept an ambiguous stale precondition.
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return CancellationRequest{}, ErrCancellationRequest
	}
	if _, err = decoder.Token(); err != io.EOF {
		return CancellationRequest{}, ErrCancellationRequest
	}
	if err := request.Validate(); err != nil {
		return CancellationRequest{}, err
	}
	return request, nil
}

// ExecutionAdapter exposes only implemented operations. Creation/preparation,
// waiting and route/auth wiring remain separate; there are no success-shaped
// placeholder methods for them. The store authorizer is mandatory on every call.
type ExecutionAdapter struct{ store *mysql.ExecutionStore }

func NewExecutionAdapter(store *mysql.ExecutionStore) (*ExecutionAdapter, error) {
	if store == nil {
		return nil, errors.New("execution store required")
	}
	return &ExecutionAdapter{store}, nil
}
func (a *ExecutionAdapter) GetScheduled(ctx context.Context, tenant, scenario, id string) (out ScheduledExecutionResponse, err error) {
	if a == nil || a.store == nil {
		return out, mysql.ErrDecisionAuthorizationUnavailable
	}
	err = a.store.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
		v, e := r.Scheduled.GetByID(ctx, tenant, scenario, id)
		if e == nil {
			out = AdaptScheduled(v)
		}
		return e
	})
	if err != nil {
		return ScheduledExecutionResponse{}, err
	}
	return out, nil
}
func (a *ExecutionAdapter) GetAsync(ctx context.Context, tenant, id string) (out AsyncExecutionResponse, err error) {
	if a == nil || a.store == nil {
		return out, mysql.ErrDecisionAuthorizationUnavailable
	}
	err = a.store.Run(ctx, mysql.ExecutionRead, func(r mysql.ExecutionRepositories) error {
		v, e := r.Async.GetByID(ctx, tenant, id)
		if e == nil {
			out = AdaptAsync(v)
		}
		return e
	})
	if err != nil {
		return AsyncExecutionResponse{}, err
	}
	return out, nil
}
func (a *ExecutionAdapter) CancelScheduled(ctx context.Context, tenant, scenario, id string, request CancellationRequest) (out ScheduledExecutionResponse, err error) {
	resource, err := cancellationID(id, request)
	if err != nil {
		return out, err
	}
	if a == nil || a.store == nil {
		return out, mysql.ErrDecisionAuthorizationUnavailable
	}
	err = a.store.Run(ctx, mysql.ExecutionCancel, func(r mysql.ExecutionRepositories) error {
		// Verify caller tenant/scenario binding before a UUID-only mutation.
		if _, err := r.Scheduled.GetByID(ctx, tenant, scenario, id); err != nil {
			return err
		}
		v, e := r.Scheduled.Cancel(ctx, resource, *request.ExpectedGeneration)
		if e == nil {
			out = AdaptScheduled(v)
		}
		return e
	})
	if err != nil {
		return ScheduledExecutionResponse{}, err
	}
	return out, nil
}
func (a *ExecutionAdapter) CancelAsync(ctx context.Context, tenant, id string, request CancellationRequest) (AsyncExecutionResponse, error) {
	return a.cancelAsync(ctx, tenant, id, request, false)
}
func (a *ExecutionAdapter) CancelCallback(ctx context.Context, tenant, id string, request CancellationRequest) (AsyncExecutionResponse, error) {
	return a.cancelAsync(ctx, tenant, id, request, true)
}
func (a *ExecutionAdapter) cancelAsync(ctx context.Context, tenant, id string, request CancellationRequest, callback bool) (out AsyncExecutionResponse, err error) {
	resource, err := cancellationID(id, request)
	if err != nil {
		return out, err
	}
	if a == nil || a.store == nil {
		return out, mysql.ErrDecisionAuthorizationUnavailable
	}
	err = a.store.Run(ctx, mysql.ExecutionCancel, func(r mysql.ExecutionRepositories) error {
		if _, err := r.Async.GetByID(ctx, tenant, id); err != nil {
			return err
		}
		var v mysql.AsyncExecutionRecord
		var e error
		if callback {
			v, e = r.Async.CancelCallback(ctx, resource, *request.ExpectedGeneration)
		} else {
			v, e = r.Async.Cancel(ctx, resource, *request.ExpectedGeneration)
		}
		if e == nil {
			out = AdaptAsync(v)
		}
		return e
	})
	if err != nil {
		return AsyncExecutionResponse{}, err
	}
	return out, nil
}
func cancellationID(id string, request CancellationRequest) (uuid.UUID, error) {
	if err := request.Validate(); err != nil {
		return uuid.Nil, err
	}
	v, err := uuid.Parse(id)
	if err != nil || v == uuid.Nil {
		return uuid.Nil, mysql.ErrTenantScope
	}
	return v, nil
}
