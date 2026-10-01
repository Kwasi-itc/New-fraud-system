package service

import (
	"context"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

func (s CaseService) lockInboxUpdate(ctx context.Context, tenantID, id uuid.UUID, target *uuid.UUID) (casepkg.Inbox, error) {
	ids := []uuid.UUID{id}
	if target != nil {
		if err := requireIDs(*target); err != nil {
			return casepkg.Inbox{}, err
		}
		ids = append(ids, *target)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	var own casepkg.Inbox
	for _, next := range ids {
		if next == id {
			item, err := s.inboxes.LockUpdate(ctx, tenantID, id)
			if err != nil {
				return own, err
			}
			own = item
		} else {
			if _, err := s.activeInbox(ctx, tenantID, next); err != nil {
				return own, err
			}
		}
	}
	return own, nil
}

func (s CaseService) inboxAccess(ctx context.Context, tenantID, inboxID uuid.UUID) error {
	p, err := access.Tenant(ctx, tenantID)
	if err != nil {
		return err
	}
	if p.Kind == access.Service || p.Admin {
		return nil
	}
	ok, err := s.memberships.Has(ctx, tenantID, inboxID, p.Subject)
	if err != nil {
		return err
	}
	if !ok {
		return casepkg.ErrForbidden
	}
	return nil
}
func (s CaseService) eligibleAssignee(ctx context.Context, tenantID, inboxID uuid.UUID, user *string) error {
	if err := validActor(user); err != nil {
		return err
	}
	if user == nil {
		return nil
	}
	ok, err := s.memberships.Has(ctx, tenantID, inboxID, *user)
	if err != nil {
		return err
	}
	if !ok {
		return casepkg.Invalid("assignee must be an inbox member")
	}
	return nil
}
func (s CaseService) loadDecisions(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) error {
	missing := make([]uuid.UUID, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil {
			return casepkg.Invalid("decision ID is required")
		}
		if _, ok := s.decisionRefs[id]; !ok && !seen[id] {
			missing = append(missing, id)
			seen[id] = true
		}
	}
	if len(missing) == 0 {
		return nil
	}
	refs, err := s.references.Decisions(ctx, tenantID, missing)
	if err != nil {
		return err
	}
	for id, ref := range refs {
		s.decisionRefs[id] = ref
	}
	return nil
}

// investigate applies human action effects inside the command transaction. Service
// integrations do not impersonate investigators or automatically claim cases.
func (s CaseService) investigate(ctx context.Context, item *casepkg.Case, advance bool) error {
	actor := access.Actor(ctx)
	if actor == nil {
		return nil
	}
	now := s.clock.Now()
	if err := s.contributors.Add(ctx, item.TenantID, item.ID, *actor, now); err != nil {
		return err
	}
	changed := false
	event := func(kind, next, prev string) error {
		_, err := s.events.Create(ctx, newEvent(s.ids.New(), item.TenantID, item.ID, actor, kind, "", "", "", next, prev, now))
		return err
	}
	if advance && item.Status == casepkg.StatusPending {
		if err := event("status_updated", string(casepkg.StatusInvestigating), string(item.Status)); err != nil {
			return err
		}
		item.Status = casepkg.StatusInvestigating
		changed = true
	}
	if item.AssignedTo == nil {
		eligible, err := s.memberships.Has(ctx, item.TenantID, item.InboxID, *actor)
		if err != nil {
			return err
		}
		if eligible {
			if err := event("case_assigned", *actor, ""); err != nil {
				return err
			}
			item.AssignedTo = actor
			changed = true
		}
	}
	if item.BoostReason != nil {
		if err := event("boost_updated", "", *item.BoostReason); err != nil {
			return err
		}
		item.BoostReason = nil
		changed = true
	}
	if changed {
		item.UpdatedAt = now
		_, err := s.cases.Update(ctx, *item)
		return err
	}
	return nil
}

func (s CaseService) ListInboxUsers(ctx context.Context, tenantID, inboxID uuid.UUID) ([]casepkg.InboxUser, error) {
	if err := requireIDs(tenantID, inboxID); err != nil {
		return nil, err
	}
	if err := access.Administrator(ctx, tenantID); err != nil {
		return nil, err
	}
	if _, err := s.inboxes.Get(ctx, tenantID, inboxID); err != nil {
		return nil, err
	}
	return s.memberships.List(ctx, tenantID, inboxID)
}
func (s CaseService) PutInboxUser(ctx context.Context, tenantID, inboxID uuid.UUID, userID string, autoAssign bool, capacities ...int) (casepkg.InboxUser, error) {
	capacity := 20
	if len(capacities) > 0 {
		capacity = capacities[0]
	}
	if capacity < 0 || capacity > 1000 {
		return casepkg.InboxUser{}, casepkg.Invalid("capacity must be 0..1000")
	}
	if err := requireIDs(tenantID, inboxID); err != nil {
		return casepkg.InboxUser{}, err
	}
	if err := access.Administrator(ctx, tenantID); err != nil {
		return casepkg.InboxUser{}, err
	}
	if err := validActor(&userID); err != nil {
		return casepkg.InboxUser{}, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.InboxUser, error) {
		if _, err := tx.inboxes.LockUpdate(ctx, tenantID, inboxID); err != nil {
			return casepkg.InboxUser{}, err
		}
		member, err := tx.memberships.Put(ctx, casepkg.InboxUser{ID: tx.ids.New(), TenantID: tenantID, InboxID: inboxID, UserID: userID, Capacity: capacity, AutoAssignEnabled: autoAssign, CreatedAt: tx.clock.Now()})
		if err != nil {
			return member, err
		}
		return member, tx.memberships.RecordChange(ctx, tenantID, inboxID, access.Actor(ctx), "member_updated", userID, autoAssign, tx.clock.Now(), member.Capacity)
	})
}
func (s CaseService) RemoveInboxUser(ctx context.Context, tenantID, inboxID uuid.UUID, userID string) error {
	if err := requireIDs(tenantID, inboxID); err != nil {
		return err
	}
	if err := access.Administrator(ctx, tenantID); err != nil {
		return err
	}
	if err := validActor(&userID); err != nil {
		return err
	}
	_, err := transact(ctx, s, func(tx CaseService) (struct{}, error) {
		if _, err := tx.inboxes.LockUpdate(ctx, tenantID, inboxID); err != nil {
			return struct{}{}, err
		}
		if err := tx.memberships.Remove(ctx, tenantID, inboxID, userID); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, tx.memberships.RecordChange(ctx, tenantID, inboxID, access.Actor(ctx), "member_removed", userID, false, tx.clock.Now(), 0)
	})
	return err
}
