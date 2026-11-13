package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type ManualScreeningInput struct {
	TenantID    uuid.UUID `json:"-"`
	ScreeningID uuid.UUID `json:"screening_id"`
	CandidateID uuid.UUID `json:"candidate_id"`
	InboxID     uuid.UUID `json:"inbox_id"`
}

// One canonical case per tenant/search is the idempotency identity. Retries
// return it only after rechecking creator ownership and current case access.
func (s CaseService) InvestigateManualScreening(ctx context.Context, in ManualScreeningInput) (casepkg.Case, error) {
	p, err := access.Tenant(ctx, in.TenantID)
	if err != nil {
		return casepkg.Case{}, err
	}
	if p.Kind != access.User || p.SessionID == uuid.Nil {
		return casepkg.Case{}, casepkg.ErrForbidden
	}
	if err := requireIDs(in.ScreeningID, in.CandidateID, in.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Case, error) {
		if err := tx.intake.Lock(ctx, in.TenantID, "screening", in.ScreeningID.String()); err != nil {
			return casepkg.Case{}, err
		}
		if _, err := tx.references.ManualScreening(ctx, in.TenantID, in.ScreeningID, in.CandidateID, p.Subject, false); err != nil {
			return casepkg.Case{}, err
		}
		existing, err := tx.screenings.CanonicalCase(ctx, in.TenantID, in.ScreeningID)
		if err != nil {
			return casepkg.Case{}, err
		}
		if existing != nil {
			item, err := tx.GetCase(ctx, in.TenantID, existing.ID)
			if err != nil {
				return casepkg.Case{}, err
			}
			if item.InboxID != in.InboxID {
				return casepkg.Case{}, casepkg.ErrConflict
			}
			return item, nil
		}
		item, err := tx.CreateCase(ctx, CreateCaseInput{TenantID: in.TenantID, InboxID: in.InboxID, Name: "Manual screening investigation", Type: casepkg.TypeManualScreening}, nil)
		if err != nil {
			return casepkg.Case{}, err
		}
		eligible, err := tx.references.ManualScreening(ctx, in.TenantID, in.ScreeningID, in.CandidateID, p.Subject, true)
		if err != nil {
			return casepkg.Case{}, err
		}
		if !eligible {
			return casepkg.Case{}, casepkg.ErrConflict
		}
		bound, err := tx.screenings.BindContinuous(ctx, in.TenantID, in.ScreeningID, item.ID)
		if err != nil {
			return casepkg.Case{}, err
		}
		if !bound {
			return casepkg.Case{}, casepkg.ErrConflict
		}
		if err := tx.screenings.SyncContinuous(ctx, in.TenantID, in.ScreeningID, item.ID, tx.clock.Now()); err != nil {
			return casepkg.Case{}, err
		}
		_, err = tx.events.Create(ctx, newEvent(tx.ids.New(), in.TenantID, item.ID, &p.Subject, "screening_added", "Opened from a stored manual search; provider was not rerun.", in.ScreeningID.String(), "manual_screening", "", "", tx.clock.Now()))
		return item, err
	})
}
