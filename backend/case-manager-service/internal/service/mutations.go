package service

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/google/uuid"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
)

// Public mutation boundaries ensure every nested write shares one transaction.
func (s CaseService) CreateInbox(ctx context.Context, in CreateInboxInput) (casepkg.Inbox, error) {
	if err := access.Administrator(ctx, in.TenantID); err != nil {
		return casepkg.Inbox{}, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Inbox, error) { return tx.createInbox(ctx, in) })
}

func (s CaseService) UpdateInbox(ctx context.Context, in UpdateInboxInput) (casepkg.Inbox, error) {
	if err := access.Administrator(ctx, in.TenantID); err != nil {
		return casepkg.Inbox{}, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Inbox, error) { return tx.updateInbox(ctx, in) })
}

func (s CaseService) CreateCase(ctx context.Context, in CreateCaseInput, actorID *string) (casepkg.Case, error) {
	if _, err := access.Tenant(ctx, in.TenantID); err != nil {
		return casepkg.Case{}, err
	}
	actorID = access.Actor(ctx)
	return transact(ctx, s, func(tx CaseService) (casepkg.Case, error) { return tx.createCase(ctx, in, actorID) })
}

func (s CaseService) UpdateCase(ctx context.Context, in UpdateCaseInput, actorID *string) (casepkg.Case, error) {
	if _, err := access.Tenant(ctx, in.TenantID); err != nil {
		return casepkg.Case{}, err
	}
	actorID = access.Actor(ctx)
	return transact(ctx, s, func(tx CaseService) (casepkg.Case, error) { return tx.updateCase(ctx, in, actorID) })
}

func (s CaseService) AddDecision(ctx context.Context, in AddDecisionInput, actorID *string) (casepkg.DecisionLink, error) {
	if _, err := access.Tenant(ctx, in.TenantID); err != nil {
		return casepkg.DecisionLink{}, err
	}
	actorID = access.Actor(ctx)
	return transact(ctx, s, func(tx CaseService) (casepkg.DecisionLink, error) { return tx.addDecision(ctx, in, actorID) })
}

func (s CaseService) Assign(ctx context.Context, tenantID, caseID uuid.UUID, assignee *string, actorID *string) error {
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return err
	}
	actorID = access.Actor(ctx)
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		return struct{}{}, tx.assign(ctx, tenantID, caseID, assignee, actorID)
	})
	return err
}

func (s CaseService) Snooze(ctx context.Context, tenantID, caseID uuid.UUID, until *time.Time, actorID *string) error {
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return err
	}
	actorID = access.Actor(ctx)
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		return struct{}{}, tx.snooze(ctx, tenantID, caseID, until, actorID)
	})
	return err
}

func (s CaseService) CreateComment(ctx context.Context, tenantID, caseID uuid.UUID, comment string, actorID *string) (casepkg.Event, error) {
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return casepkg.Event{}, err
	}
	actorID = access.Actor(ctx)
	return transact(ctx, s, func(tx CaseService) (casepkg.Event, error) {
		return tx.createComment(ctx, tenantID, caseID, comment, actorID)
	})
}

func (s CaseService) CreateTag(ctx context.Context, in CreateTagInput) (casepkg.Tag, error) {
	if err := access.Administrator(ctx, in.TenantID); err != nil {
		return casepkg.Tag{}, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Tag, error) { return tx.createTag(ctx, in) })
}

func (s CaseService) AddTag(ctx context.Context, tenantID, caseID, tagID uuid.UUID, actorID *string) error {
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return err
	}
	actorID = access.Actor(ctx)
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		return struct{}{}, tx.addTag(ctx, tenantID, caseID, tagID, actorID)
	})
	return err
}

func (s CaseService) RemoveTag(ctx context.Context, tenantID, caseID, tagID uuid.UUID, actorID *string) error {
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return err
	}
	actorID = access.Actor(ctx)
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		return struct{}{}, tx.removeTag(ctx, tenantID, caseID, tagID, actorID)
	})
	return err
}

func (s CaseService) AddFile(ctx context.Context, file casepkg.File, actorID *string) (casepkg.File, error) {
	if _, err := access.Tenant(ctx, file.TenantID); err != nil {
		return casepkg.File{}, err
	}
	actorID = access.Actor(ctx)
	return transact(ctx, s, func(tx CaseService) (casepkg.File, error) { return tx.addFile(ctx, file, actorID) })
}

func (s CaseService) HandleWorkflowAction(ctx context.Context, in WorkflowActionInput) (casepkg.Case, error) {
	if err := access.Integration(ctx, in.TenantID); err != nil {
		return casepkg.Case{}, err
	}
	id, err := s.receive(ctx, in.TenantID, "workflow", in.WorkflowExecutionID, in, func(tx CaseService) (uuid.UUID, error) {
		item, err := tx.handleWorkflowAction(ctx, in)
		return item.ID, err
	})
	if err != nil {
		return casepkg.Case{}, err
	}
	return s.GetCase(ctx, in.TenantID, id)
}

func (s CaseService) HandleScreeningReviewed(ctx context.Context, tenantID, screeningID uuid.UUID, decisionID *uuid.UUID, matchID string, status string, reviewerID *string) error {
	if err := access.Integration(ctx, tenantID); err != nil {
		return err
	}
	reviewerID = nil // Service callbacks do not establish a human identity.
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		return struct{}{}, tx.handleScreeningReviewed(ctx, tenantID, screeningID, decisionID, matchID, status, reviewerID)
	})
	return err
}
