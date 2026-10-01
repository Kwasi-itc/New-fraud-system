package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"sort"
	"strings"
)

func (s CaseService) EvidenceAccess(ctx context.Context, tenant, id uuid.UUID, kind string, resource uuid.UUID) error {
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		if _, err := tx.workspaceCase(ctx, tenant, id); err != nil {
			return struct{}{}, err
		}
		found, err := tx.workspace.HasLink(ctx, tenant, id, kind, resource)
		if err != nil {
			return struct{}{}, err
		}
		if !found {
			return struct{}{}, casepkg.ErrNotFound
		}
		return struct{}{}, nil
	})
	return err
}

func (s CaseService) WorkspaceQueue(ctx context.Context, tenant uuid.UUID, f casepkg.WorkspaceFilters, limit int) (casepkg.QueuePage, error) {
	empty := casepkg.QueuePage{}
	if err := requireIDs(tenant); err != nil {
		return empty, err
	}
	if err := validLimit(limit); err != nil {
		return empty, err
	}
	for _, v := range f.Statuses {
		if !casepkg.ValidStatus(v) {
			return empty, casepkg.Invalid("invalid status")
		}
	}
	for _, v := range f.InboxIDs {
		if err := requireIDs(v); err != nil {
			return empty, err
		}
	}
	if f.ReviewLevel != "" && !casepkg.ValidReviewLevel(f.ReviewLevel) {
		return empty, casepkg.Invalid("invalid review level")
	}
	if f.Unassigned && f.AssigneeID != "" {
		return empty, casepkg.Invalid("conflicting assignment filters")
	}
	if f.CreatedFrom != nil && f.CreatedTo != nil && !f.CreatedFrom.Before(*f.CreatedTo) {
		return empty, casepkg.Invalid("created_from must precede created_to")
	}
	p, err := access.Tenant(ctx, tenant)
	if err != nil {
		return empty, err
	}
	f.AccessUserID = ""
	if p.Kind == access.User && !p.Admin {
		f.AccessUserID = p.Subject
	}
	if f.RelatedTo != nil {
		if _, err := s.WorkspaceOverview(ctx, tenant, *f.RelatedTo); err != nil {
			return empty, err
		}
	}
	result, err := s.workspace.Queue(ctx, tenant, f, limit)
	if err != nil {
		return empty, err
	}
	ids := make([]uuid.UUID, len(result.Cases))
	for i, item := range result.Cases {
		ids[i] = item.ID
	}
	tags, err := s.tags.ListByCases(ctx, tenant, ids)
	if err != nil {
		return empty, err
	}
	for i := range result.Cases {
		result.Cases[i].Tags = tags[result.Cases[i].ID]
	}
	return result, nil
}

func (s CaseService) workspaceCase(ctx context.Context, tenant, id uuid.UUID) (casepkg.Case, error) {
	if err := requireIDs(tenant, id); err != nil {
		return casepkg.Case{}, err
	}
	if _, err := access.Tenant(ctx, tenant); err != nil {
		return casepkg.Case{}, err
	}
	item, err := s.cases.LockShared(ctx, tenant, id)
	if err != nil {
		return item, err
	}
	inbox, err := s.inboxes.LockShared(ctx, tenant, item.InboxID)
	if err != nil {
		return item, err
	}
	item.SLADueAt = casepkg.SLADueAt(item.CreatedAt, inbox.SLADays)
	return item, s.inboxAccess(ctx, tenant, item.InboxID)
}
func (s CaseService) WorkspaceOverview(ctx context.Context, tenant, id uuid.UUID) (casepkg.Case, error) {
	return transact(ctx, s, func(tx CaseService) (casepkg.Case, error) {
		item, err := tx.workspaceCase(ctx, tenant, id)
		if err != nil {
			return item, err
		}
		item.Tags, err = tx.tags.ListByCase(ctx, tenant, id)
		if err != nil {
			return item, err
		}
		item.Contributors, err = tx.contributors.List(ctx, tenant, id)
		return item, err
	})
}
func (s CaseService) WorkspaceLinks(ctx context.Context, tenant, id uuid.UUID, kind string, before *casepkg.Cursor, limit int) (casepkg.LinkPage, error) {
	if err := validLimit(limit); err != nil {
		return casepkg.LinkPage{}, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.LinkPage, error) {
		if _, err := tx.workspaceCase(ctx, tenant, id); err != nil {
			return casepkg.LinkPage{}, err
		}
		return tx.workspace.Links(ctx, tenant, id, kind, before, limit)
	})
}

// Escalation deliberately permits the configured destination without requiring
// destination membership. It never returns destination data after the move.
func (s CaseService) EscalateCase(ctx context.Context, tenant, id uuid.UUID) error {
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		if err := requireIDs(tenant, id); err != nil {
			return struct{}{}, err
		}
		if _, err := access.Tenant(ctx, tenant); err != nil {
			return struct{}{}, err
		}
		item, err := tx.cases.Lock(ctx, tenant, id)
		if err != nil {
			return struct{}{}, err
		}
		if item.Status == casepkg.StatusClosed {
			return struct{}{}, casepkg.Invalid("closed cases cannot be escalated")
		}
		source, err := tx.inboxes.Get(ctx, tenant, item.InboxID)
		if err != nil {
			return struct{}{}, err
		}
		if source.EscalationInboxID == nil {
			return struct{}{}, casepkg.Invalid("no escalation destination configured")
		}
		destination := *source.EscalationInboxID
		ids := []uuid.UUID{source.ID, destination}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		for _, inbox := range ids {
			if _, err := tx.activeInbox(ctx, tenant, inbox); err != nil {
				return struct{}{}, err
			}
		}
		locked, err := tx.inboxes.Get(ctx, tenant, source.ID)
		if err != nil {
			return struct{}{}, err
		}
		if locked.EscalationInboxID == nil || *locked.EscalationInboxID != destination {
			return struct{}{}, casepkg.ErrConflict
		}
		if destination == source.ID {
			return struct{}{}, casepkg.Invalid("escalation destination must differ")
		}
		if err := tx.inboxAccess(ctx, tenant, source.ID); err != nil {
			return struct{}{}, err
		}
		actor := access.Actor(ctx)
		now := tx.clock.Now()
		if actor != nil {
			if err := tx.contributors.Add(ctx, tenant, id, *actor, now); err != nil {
				return struct{}{}, err
			}
		}
		boost := "escalated"
		item.InboxID = destination
		item.AssignedTo = nil
		item.SnoozedUntil = nil
		item.BoostReason = &boost
		item.ReviewLevel = nil
		item.UpdatedAt = now
		if _, err := tx.cases.Update(ctx, item); err != nil {
			return struct{}{}, err
		}
		_, err = tx.events.Create(ctx, newEvent(tx.ids.New(), tenant, id, actor, "case_escalated", "Assignment and snooze cleared", destination.String(), "inbox", destination.String(), source.ID.String(), now))
		return struct{}{}, err
	})
	return err
}

func (s CaseService) CloseCase(ctx context.Context, tenant, id uuid.UUID, outcome casepkg.Outcome, comment string) (casepkg.Case, error) {
	if strings.TrimSpace(comment) == "" {
		return casepkg.Case{}, casepkg.Invalid("a closing comment is required")
	}
	if !casepkg.ValidOutcome(outcome) || outcome == casepkg.OutcomeUnset {
		return casepkg.Case{}, casepkg.Invalid("a closing outcome is required")
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Case, error) {
		if _, err := tx.CreateComment(ctx, tenant, id, comment, access.Actor(ctx)); err != nil {
			return casepkg.Case{}, err
		}
		status := casepkg.StatusClosed
		return tx.UpdateCase(ctx, UpdateCaseInput{TenantID: tenant, CaseID: id, Status: &status, Outcome: &outcome}, access.Actor(ctx))
	})
}
