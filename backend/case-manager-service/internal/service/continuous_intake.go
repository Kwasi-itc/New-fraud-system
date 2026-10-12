package service

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
)

type ScreeningArrivalInput struct {
	Version     int       `json:"version"`
	EventID     uuid.UUID `json:"event_id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	ScreeningID uuid.UUID `json:"screening_id"`
}

func (s CaseService) ReceiveScreeningArrival(ctx context.Context, in ScreeningArrivalInput) error {
	if in.Version != 1 {
		return casepkg.Invalid("unsupported screening arrival version")
	}
	if err := requireIDs(in.TenantID, in.ScreeningID, in.EventID); err != nil {
		return err
	}
	expected := uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening.requires-review/v1/"+in.TenantID.String()+"/"+in.ScreeningID.String()))
	if in.EventID != expected {
		return casepkg.Invalid("event identity does not match screening run")
	}
	_, err := s.receive(ctx, in.TenantID, "screening.requires-review", in.EventID, in, func(tx CaseService) (uuid.UUID, error) {
		if err := tx.intake.Lock(ctx, in.TenantID, "screening", in.ScreeningID.String()); err != nil {
			return uuid.Nil, err
		}
		ref, err := tx.references.ContinuousScreening(ctx, in.TenantID, in.ScreeningID)
		if err != nil {
			return uuid.Nil, err
		}
		if ref == nil {
			decision, err := tx.references.DecisionScreening(ctx, in.TenantID, in.ScreeningID)
			if err != nil {
				return uuid.Nil, err
			}
			if decision == nil {
				return uuid.Nil, casepkg.ErrNotFound
			}
			item, err := tx.ensureDecisionScreeningCase(ctx, in.TenantID, in.ScreeningID, *decision)
			return item.ID, err
		}
		item, err := tx.ensureContinuousCase(ctx, in.TenantID, in.ScreeningID, *ref)
		return item.ID, err
	})
	return err
}

// Caller holds the per-screening intake lock in the same transaction.
func (s CaseService) ensureContinuousCase(ctx context.Context, tenant, id uuid.UUID, ref ports.ContinuousScreeningReference) (casepkg.Case, error) {
	if !ref.Published {
		return casepkg.Case{}, casepkg.Invalid("screening has no published review arrival")
	}
	item, err := s.screenings.CanonicalCase(ctx, tenant, id)
	if err != nil {
		return casepkg.Case{}, err
	}
	if item == nil {
		if !ref.Enabled || ref.ReviewInboxID == nil {
			return casepkg.Case{}, casepkg.Invalid("continuous screening has no active configured review inbox")
		}
		inbox, err := uuid.Parse(*ref.ReviewInboxID)
		if err != nil || inbox == uuid.Nil {
			return casepkg.Case{}, casepkg.Invalid("invalid configured review inbox")
		}
		created, err := s.CreateCase(ctx, CreateCaseInput{TenantID: tenant, InboxID: inbox, Name: "Screening review", Type: casepkg.TypeContinuousScreening}, nil)
		if err != nil {
			return casepkg.Case{}, err
		}
		item = &created
	}
	// Existing cases retain assignment, inbox and lifecycle state, including closed.
	if _, err := s.caseForMutation(ctx, tenant, item.ID, nil); err != nil {
		return casepkg.Case{}, err
	}
	bound, err := s.screenings.BindContinuous(ctx, tenant, id, item.ID)
	if err != nil {
		return casepkg.Case{}, err
	}
	if err := s.screenings.SyncContinuous(ctx, tenant, id, item.ID, s.clock.Now()); err != nil {
		return casepkg.Case{}, err
	}
	if bound {
		if _, err := s.events.Create(ctx, newEvent(s.ids.New(), tenant, item.ID, nil, "screening_added", "", id.String(), "continuous_screening", "", "", s.clock.Now())); err != nil {
			return casepkg.Case{}, err
		}
	}
	return *item, nil
}
