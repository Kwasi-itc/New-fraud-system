package mysqlapi

import (
	"context"
	"errors"
	"io"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

var ErrRedriveRequest = errors.New("redrive requires an explicit nonnegative signed expected_generation and no other fields")

type RedriveRequest struct {
	ExpectedGeneration *int `json:"expected_generation"`
}

func (r RedriveRequest) Validate() error {
	if (CancellationRequest{r.ExpectedGeneration}).Validate() != nil {
		return ErrRedriveRequest
	}
	return nil
}
func DecodeRedrive(body io.Reader) (RedriveRequest, error) {
	request, err := DecodeCancellation(body)
	if err != nil {
		return RedriveRequest{}, errors.Join(ErrRedriveRequest, err)
	}
	return RedriveRequest{request.ExpectedGeneration}, nil
}
func (a *ExecutionAdapter) RedriveScheduled(ctx context.Context, tenant, scenario, id string, request RedriveRequest) (out ScheduledExecutionResponse, err error) {
	if err := request.Validate(); err != nil {
		return out, err
	}
	resource, err := cancellationID(id, CancellationRequest{request.ExpectedGeneration})
	if err != nil {
		return out, err
	}
	if a == nil || a.store == nil {
		return out, mysql.ErrDecisionAuthorizationUnavailable
	}
	err = a.store.Run(ctx, mysql.ExecutionRedrive, func(r mysql.ExecutionRepositories) error {
		if _, err := r.Scheduled.GetByID(ctx, tenant, scenario, id); err != nil {
			return err
		}
		v, err := r.Scheduled.Redrive(ctx, resource, *request.ExpectedGeneration)
		if err != nil {
			return err
		}
		out = AdaptScheduled(v)
		return nil
	})
	if err != nil {
		return ScheduledExecutionResponse{}, err
	}
	return out, nil
}
func (a *ExecutionAdapter) RedriveAsync(ctx context.Context, tenant, id string, request RedriveRequest) (AsyncExecutionResponse, error) {
	return a.redriveAsync(ctx, tenant, id, request, false)
}
func (a *ExecutionAdapter) RedriveCallback(ctx context.Context, tenant, id string, request RedriveRequest) (AsyncExecutionResponse, error) {
	return a.redriveAsync(ctx, tenant, id, request, true)
}
func (a *ExecutionAdapter) redriveAsync(ctx context.Context, tenant, id string, request RedriveRequest, callback bool) (out AsyncExecutionResponse, err error) {
	if err := request.Validate(); err != nil {
		return out, err
	}
	resource, err := cancellationID(id, CancellationRequest{request.ExpectedGeneration})
	if err != nil {
		return out, err
	}
	if a == nil || a.store == nil {
		return out, mysql.ErrDecisionAuthorizationUnavailable
	}
	err = a.store.Run(ctx, mysql.ExecutionRedrive, func(r mysql.ExecutionRepositories) error {
		if _, err := r.Async.GetByID(ctx, tenant, id); err != nil {
			return err
		}
		var v mysql.AsyncExecutionRecord
		var err error
		if callback {
			v, err = r.Async.RedriveCallback(ctx, resource, *request.ExpectedGeneration)
		} else {
			v, err = r.Async.Redrive(ctx, resource, *request.ExpectedGeneration)
		}
		if err != nil {
			return err
		}
		out = AdaptAsync(v)
		return nil
	})
	if err != nil {
		return AsyncExecutionResponse{}, err
	}
	return out, nil
}
