package service

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
)

// WorkflowReceiptMutation is the case owner's narrow transactional capability.
// ApplyWorkflow must enforce case business rules and persist case/link/tag/audit
// and notification writes in the same transaction as the intake receipts.
type WorkflowReceiptMutation interface {
	ports.IntakeRepository
	ApplyWorkflow(context.Context, WorkflowActionInput) (uuid.UUID, error)
}

type WorkflowReceiptStore interface {
	WithinWorkflow(context.Context, func(WorkflowReceiptMutation) error) error
	Receipt(context.Context, uuid.UUID, string, uuid.UUID) (*ports.IntakeReceipt, error)
}

type WorkflowReceiptService struct {
	store WorkflowReceiptStore
	clock ports.Clock
}

func NewWorkflowReceiptService(store WorkflowReceiptStore, clock ports.Clock) (WorkflowReceiptService, error) {
	if store == nil || clock == nil {
		return WorkflowReceiptService{}, errors.New("workflow receipt store and clock required")
	}
	return WorkflowReceiptService{store: store, clock: clock}, nil
}

func (s WorkflowReceiptService) DeliverWorkflowReceipt(ctx context.Context, r WorkflowDispatchRequest) (WorkflowDispatchReceipt, error) {
	tenant, id, err := r.validate()
	if err != nil {
		return WorkflowDispatchReceipt{}, err
	}
	if err := access.Integration(ctx, tenant); err != nil {
		return WorkflowDispatchReceipt{}, err
	}
	in, err := r.input()
	if err != nil {
		return WorkflowDispatchReceipt{}, err
	}
	var result WorkflowDispatchReceipt
	err = s.store.WithinWorkflow(ctx, func(tx WorkflowReceiptMutation) error {
		if err := tx.Lock(ctx, tenant, "workflow.receipt.v1", id.String()); err != nil {
			return err
		}
		base, err := tx.Get(ctx, tenant, "workflow.payload.v1", id)
		if err != nil {
			return err
		}
		if base != nil && base.PayloadHash != r.PayloadSHA256 {
			return casepkg.ErrConflict
		}
		prior, err := tx.Get(ctx, tenant, r.receiptSource(), id)
		if err != nil {
			return err
		}
		if prior != nil {
			if base == nil || prior.PayloadHash != r.PayloadSHA256 || prior.CaseID != base.CaseID {
				return casepkg.ErrConflict
			}
			result = r.result(prior.CaseID)
			return nil
		}
		var caseID uuid.UUID
		if base != nil {
			caseID = base.CaseID
		} else {
			caseID, err = tx.ApplyWorkflow(ctx, in)
			if err != nil {
				return err
			}
			if caseID == uuid.Nil {
				return errors.New("workflow mutation returned no case")
			}
			if err := tx.Create(ctx, ports.IntakeReceipt{TenantID: tenant, Source: "workflow.payload.v1", EventID: id, PayloadHash: r.PayloadSHA256, CaseID: caseID, CreatedAt: s.clock.Now()}); err != nil {
				return err
			}
		}
		if err := tx.Create(ctx, ports.IntakeReceipt{TenantID: tenant, Source: r.receiptSource(), EventID: id, PayloadHash: r.PayloadSHA256, CaseID: caseID, CreatedAt: s.clock.Now()}); err != nil {
			return err
		}
		result = r.result(caseID)
		return nil
	})
	if err != nil {
		return WorkflowDispatchReceipt{}, err
	}
	return result, nil
}

func (s WorkflowReceiptService) ReconcileWorkflowReceipt(ctx context.Context, r WorkflowDispatchRequest) (WorkflowDispatchReceipt, error) {
	tenant, id, err := r.validate()
	if err != nil {
		return WorkflowDispatchReceipt{}, err
	}
	if err := access.Integration(ctx, tenant); err != nil {
		return WorkflowDispatchReceipt{}, err
	}
	prior, err := s.store.Receipt(ctx, tenant, r.receiptSource(), id)
	if err != nil {
		return WorkflowDispatchReceipt{}, err
	}
	if prior == nil {
		return r.result(uuid.Nil), nil
	}
	if prior.PayloadHash != r.PayloadSHA256 {
		return WorkflowDispatchReceipt{}, casepkg.ErrConflict
	}
	return r.result(prior.CaseID), nil
}

type caseWorkflowReceiptStore struct{ service CaseService }
type caseWorkflowReceiptMutation struct {
	ports.IntakeRepository
	service CaseService
}

func (s caseWorkflowReceiptStore) WithinWorkflow(ctx context.Context, fn func(WorkflowReceiptMutation) error) error {
	_, err := transact(ctx, s.service, func(tx CaseService) (struct{}, error) {
		return struct{}{}, fn(caseWorkflowReceiptMutation{tx.intake, tx})
	})
	return err
}
func (s caseWorkflowReceiptStore) Receipt(ctx context.Context, tenant uuid.UUID, source string, id uuid.UUID) (*ports.IntakeReceipt, error) {
	return s.service.intake.Get(ctx, tenant, source, id)
}
func (s caseWorkflowReceiptMutation) ApplyWorkflow(ctx context.Context, in WorkflowActionInput) (uuid.UUID, error) {
	item, err := s.service.HandleWorkflowAction(ctx, in)
	return item.ID, err
}
