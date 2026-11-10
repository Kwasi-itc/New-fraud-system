package service

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

// Optional owner contract: PostgreSQL keeps its existing unsupported callback
// until that adapter implements authoritative evidence publication reads.
type screeningEvidenceOwner interface {
	PublishedEvidence(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (casepkg.File, *uuid.UUID, error)
	ImportedEvidence(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*casepkg.File, error)
}

type ScreeningEvidenceInput struct {
	EventID     uuid.UUID `json:"event_id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	ScreeningID uuid.UUID `json:"screening_id"`
	FileID      uuid.UUID `json:"file_id"`
	UploadedBy  string    `json:"uploaded_by,omitempty"`
}

func (s CaseService) SupportsScreeningEvidence() bool {
	_, ok := s.references.(screeningEvidenceOwner)
	return ok
}

func (s CaseService) ReceiveScreeningEvidence(ctx context.Context, in ScreeningEvidenceInput) error {
	if err := requireIDs(in.EventID, in.TenantID, in.ScreeningID, in.FileID); err != nil {
		return err
	}
	if in.EventID != in.FileID {
		return casepkg.Invalid("event identity does not match source file")
	}
	_, err := s.receive(ctx, in.TenantID, "screening.evidence", in.EventID, in, func(tx CaseService) (uuid.UUID, error) {
		if err := tx.intake.Lock(ctx, in.TenantID, "screening", in.ScreeningID.String()); err != nil {
			return uuid.Nil, err
		}
		owner, ok := tx.references.(screeningEvidenceOwner)
		if !ok {
			return uuid.Nil, casepkg.ErrUnavailable
		}
		file, decision, err := owner.PublishedEvidence(ctx, in.TenantID, in.ScreeningID, in.FileID, in.EventID)
		if err != nil {
			return uuid.Nil, err
		}
		if in.UploadedBy != "" && in.UploadedBy != file.UploadedBy {
			return uuid.Nil, casepkg.Invalid("uploader does not match source file")
		}
		item, err := tx.screenings.CanonicalCase(ctx, in.TenantID, in.ScreeningID)
		if err != nil {
			return uuid.Nil, err
		}
		if item == nil && decision != nil {
			item, err = tx.decisions.FindCase(ctx, in.TenantID, *decision)
			if err != nil {
				return uuid.Nil, err
			}
		}
		if item == nil {
			return uuid.Nil, casepkg.ErrNotFound
		} // Retain callback for later case linkage.
		if _, err = tx.caseForMutation(ctx, in.TenantID, item.ID, nil); err != nil {
			return uuid.Nil, err
		}
		file.CaseID = item.ID
		created, err := tx.addFile(ctx, file, nil)
		if err != nil {
			return uuid.Nil, err
		}
		// Import, byte/hash verification, audit/outbox and the receipt commit together.
		if _, err = tx.uploads.Lock(ctx, in.TenantID, item.ID, created.ID); err != nil {
			return uuid.Nil, err
		}
		return item.ID, nil
	})
	return err
}
