// Package screeningreceipt binds decision dispatch to the screening-owned
// same-database intake contract. All writes remain inside the screening owner.
package screeningreceipt

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlpublication"
	"github.com/google/uuid"
)

type Local struct{ owner *mysqlpublication.Intake }

var _ ports.ReceiptDispatcher = (*Local)(nil)

func New(owner *mysqlpublication.Intake) (*Local, error) {
	if owner == nil {
		return nil, errors.New("screening receipt owner required")
	}
	return &Local{owner}, nil
}
func request(r ports.DispatchRequest) (mysqlpublication.IntakeRequest, error) {
	tenant, e1 := uuid.Parse(r.TenantID)
	execution, e2 := uuid.Parse(r.ResourceID)
	if e1 != nil || e2 != nil || r.Kind != ports.JobScreening || tenant.String() != r.TenantID || execution.String() != r.ResourceID {
		return mysqlpublication.IntakeRequest{}, mysqlpublication.ErrInvalid
	}
	return mysqlpublication.IntakeRequest{Key: r.Key, PayloadSHA256: r.PayloadSHA256, TenantID: tenant, ExecutionID: execution, DomainAttempt: r.DomainAttempt, RedriveGeneration: r.RedriveGeneration, Payload: r.Payload}, nil
}
func receipt(r mysqlpublication.IntakeReceipt) ports.DispatchReceipt {
	out := ports.DispatchReceipt{Key: r.Key, PayloadSHA256: r.PayloadSHA256, Outcome: ports.DispatchNotFound}
	if r.Found {
		out.Outcome = ports.DispatchAccepted
		out.Reference = r.ScreeningID.String()
	}
	return out
}
func (s *Local) Deliver(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	if s == nil || s.owner == nil {
		return ports.DispatchReceipt{}, mysqlpublication.ErrInvalid
	}
	in, err := request(r)
	if err != nil {
		return ports.DispatchReceipt{}, err
	}
	out, err := s.owner.Deliver(ctx, in)
	if err != nil {
		return ports.DispatchReceipt{}, err
	}
	return receipt(out), nil
}
func (s *Local) Reconcile(ctx context.Context, r ports.DispatchRequest) (ports.DispatchReceipt, error) {
	if s == nil || s.owner == nil {
		return ports.DispatchReceipt{}, mysqlpublication.ErrInvalid
	}
	in, err := request(r)
	if err != nil {
		return ports.DispatchReceipt{}, err
	}
	out, err := s.owner.Reconcile(ctx, in)
	if err != nil {
		return ports.DispatchReceipt{}, err
	}
	return receipt(out), nil
}
