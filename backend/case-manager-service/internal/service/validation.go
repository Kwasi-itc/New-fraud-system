package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

func requireIDs(ids ...uuid.UUID) error {
	for _, id := range ids {
		if id == uuid.Nil {
			return casepkg.Invalid("IDs must not be zero UUIDs")
		}
	}
	return nil
}

func validActor(actor *string) error {
	if actor != nil && (strings.TrimSpace(*actor) == "" || strings.TrimSpace(*actor) != *actor) {
		return casepkg.Invalid("user ID must be nonempty and have no surrounding whitespace")
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validLimit(limit int) error {
	if limit < 1 || limit > 500 {
		return casepkg.Invalid("limit must be between 1 and 500")
	}
	return nil
}

func validateDecisionMetadata(ref casepkg.DecisionReference, scenarioID *uuid.UUID, objectType, objectID string) error {
	if scenarioID != nil && *scenarioID != ref.ScenarioID {
		return casepkg.Invalid("decision scenario does not match")
	}
	if (objectType != "" && objectType != ref.ObjectType) || (objectID != "" && objectID != ref.ObjectID) {
		return casepkg.Invalid("decision object does not match")
	}
	return nil
}

func (s CaseService) activeInbox(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error) {
	if err := requireIDs(tenantID, inboxID); err != nil {
		return casepkg.Inbox{}, err
	}
	inbox, err := s.inboxes.LockShared(ctx, tenantID, inboxID)
	if err != nil {
		return casepkg.Inbox{}, err
	}
	if inbox.Status != "active" {
		return casepkg.Inbox{}, casepkg.Invalid("inbox must be active")
	}
	return inbox, nil
}

func (s CaseService) caseForMutation(ctx context.Context, tenantID, caseID uuid.UUID, actor *string, destinations ...uuid.UUID) (casepkg.Case, error) {
	if err := requireIDs(tenantID, caseID); err != nil {
		return casepkg.Case{}, err
	}
	if err := validActor(actor); err != nil {
		return casepkg.Case{}, err
	}
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return casepkg.Case{}, err
	}
	item, err := s.cases.Lock(ctx, tenantID, caseID)
	if err != nil {
		return casepkg.Case{}, err
	}
	// Lock order is case -> inboxes in UUID order -> membership/reference.
	// Match inbox settings updates, including moves between two inboxes.
	// Membership mutations lock only the inbox and never acquire a case lock.
	inboxIDs := append([]uuid.UUID{item.InboxID}, destinations...)
	if err := requireIDs(inboxIDs...); err != nil {
		return casepkg.Case{}, err
	}
	sort.Slice(inboxIDs, func(i, j int) bool { return inboxIDs[i].String() < inboxIDs[j].String() })
	for i, id := range inboxIDs {
		if i > 0 && id == inboxIDs[i-1] {
			continue
		}
		if _, err := s.inboxes.LockShared(ctx, tenantID, id); err != nil {
			return casepkg.Case{}, err
		}
	}
	if err := s.inboxAccess(ctx, tenantID, item.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	return item, nil
}

func (s CaseService) caseTag(ctx context.Context, tenantID, tagID uuid.UUID) error {
	if err := requireIDs(tenantID, tagID); err != nil {
		return err
	}
	tag, err := s.tags.Get(ctx, tenantID, tagID)
	if err != nil {
		return err
	}
	if tag.DeletedAt != nil || tag.Target != "case" {
		return casepkg.Invalid("tag must be an active case tag")
	}
	return nil
}
