package service

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
)

// Caller holds the screening intake lock. The object lock matches workflow
// intake; database primary membership also arbitrates manual attachment races.
func (s CaseService) ensureDecisionScreeningCase(ctx context.Context, tenant, screening uuid.UUID, ref ports.DecisionScreeningReference) (casepkg.Case, error) {
	if !ref.Published {
		return casepkg.Case{}, casepkg.Invalid("screening has no published review arrival")
	}
	if err := s.loadDecisions(ctx, tenant, []uuid.UUID{ref.DecisionID}); err != nil {
		return casepkg.Case{}, err
	}
	decision := s.decisionRefs[ref.DecisionID]
	if err := s.intake.Lock(ctx, tenant, "pivot", decision.ObjectType+":"+decision.ObjectID); err != nil {
		return casepkg.Case{}, err
	}
	item, err := s.decisions.FindCase(ctx, tenant, ref.DecisionID)
	if err != nil {
		return casepkg.Case{}, err
	}
	if item == nil {
		if ref.ReviewInboxID == nil {
			return casepkg.Case{}, casepkg.Invalid("decision screening configuration requires a review inbox")
		}
		created, err := s.CreateCase(ctx, CreateCaseInput{TenantID: tenant, InboxID: *ref.ReviewInboxID, Name: "Screening review", Type: casepkg.TypeDecision, DecisionIDs: []uuid.UUID{ref.DecisionID}}, nil)
		if err != nil {
			return casepkg.Case{}, err
		}
		item = &created
	}
	locked, err := s.caseForMutation(ctx, tenant, item.ID, nil)
	if err != nil {
		return casepkg.Case{}, err
	}
	current, err := s.decisions.Membership(ctx, tenant, ref.DecisionID)
	if err != nil {
		return casepkg.Case{}, err
	}
	if current == nil || current.CaseID != item.ID {
		return casepkg.Case{}, casepkg.ErrConflict
	}
	linked, err := s.screenings.CanonicalCase(ctx, tenant, screening)
	if err != nil {
		return casepkg.Case{}, err
	}
	if linked != nil && linked.ID != item.ID {
		return casepkg.Case{}, casepkg.ErrConflict
	}
	if err := s.screenings.SyncContinuous(ctx, tenant, screening, item.ID, s.clock.Now()); err != nil {
		return casepkg.Case{}, err
	}
	if linked == nil {
		if _, err := s.events.Create(ctx, newEvent(s.ids.New(), tenant, item.ID, nil, "screening_added", "", screening.String(), "decision_screening", "", "", s.clock.Now())); err != nil {
			return casepkg.Case{}, err
		}
	}
	return locked, nil
}
