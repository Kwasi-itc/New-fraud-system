package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
)

func (s CaseService) receive(ctx context.Context, tenant uuid.UUID, source string, id uuid.UUID, payload any, apply func(CaseService) (uuid.UUID, error)) (uuid.UUID, error) {
	if err := access.Integration(ctx, tenant); err != nil {
		return uuid.Nil, err
	}
	if err := requireIDs(tenant, id); err != nil {
		return uuid.Nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	return transact(ctx, s, func(tx CaseService) (uuid.UUID, error) {
		if err := tx.intake.Lock(ctx, tenant, source, id.String()); err != nil {
			return uuid.Nil, err
		}
		receipt, err := tx.intake.Get(ctx, tenant, source, id)
		if err != nil {
			return uuid.Nil, err
		}
		if receipt != nil {
			if receipt.PayloadHash != hash {
				return uuid.Nil, casepkg.ErrConflict
			}
			return receipt.CaseID, nil
		}
		caseID, err := apply(tx)
		if err != nil {
			return uuid.Nil, err
		}
		err = tx.intake.Create(ctx, ports.IntakeReceipt{TenantID: tenant, Source: source, EventID: id, PayloadHash: hash, CaseID: caseID, CreatedAt: tx.clock.Now()})
		return caseID, err
	})
}

type ScreeningReviewInput struct {
	EventID     uuid.UUID  `json:"event_id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	ScreeningID uuid.UUID  `json:"screening_id"`
	DecisionID  *uuid.UUID `json:"decision_id,omitempty"`
	MatchID     string     `json:"match_id"`
	Status      string     `json:"status"`
}

func (s CaseService) ReceiveScreeningReview(ctx context.Context, in ScreeningReviewInput) error {
	_, err := s.receive(ctx, in.TenantID, "screening.reviewed", in.EventID, in, func(tx CaseService) (uuid.UUID, error) {
		// Serialize different reviews/matches of one screening as well as retries.
		if err := tx.intake.Lock(ctx, in.TenantID, "screening", in.ScreeningID.String()); err != nil {
			return uuid.Nil, err
		}
		if err := tx.handleScreeningReviewed(ctx, in.TenantID, in.ScreeningID, in.DecisionID, in.MatchID, in.Status, nil); err != nil {
			return uuid.Nil, err
		}
		item, err := tx.screenings.FindCase(ctx, in.TenantID, in.ScreeningID)
		if err != nil {
			return uuid.Nil, err
		}
		if item == nil {
			return uuid.Nil, casepkg.ErrNotFound
		}
		return item.ID, nil
	})
	return err
}
