package service

import (
	"context"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
)

const maxClosingFindingsBytes = 10000

func validateClosingFindings(outcome casepkg.Outcome, comment string) error {
	if !casepkg.ValidOutcome(outcome) || outcome == casepkg.OutcomeUnset {
		return casepkg.Invalid("a closing outcome is required")
	}
	if strings.TrimSpace(comment) == "" || len(comment) > maxClosingFindingsBytes {
		return casepkg.Invalid("closing findings must be nonblank and at most 10000 bytes")
	}
	return nil
}

// applyLifecycle operates on an already locked, authorized case in a command
// transaction. Callers persist their existing single/bulk audit format. The
// database capacity trigger enforces reopening capacity on the final update.
func (s CaseService) applyLifecycle(ctx context.Context, item *casepkg.Case, next casepkg.Status, outcome casepkg.Outcome, comment string) error {
	if access.Actor(ctx) == nil {
		return casepkg.ErrForbidden
	}
	switch next {
	case casepkg.StatusClosed:
		if err := validateClosingFindings(outcome, comment); err != nil {
			return err
		}
		if item.Status == casepkg.StatusClosed {
			return casepkg.ErrConflict
		}
	case casepkg.StatusInvestigating:
		if item.Status != casepkg.StatusClosed {
			return casepkg.ErrConflict
		}
		if _, err := s.activeInbox(ctx, item.TenantID, item.InboxID); err != nil {
			return err
		}
		if err := s.eligibleAssignee(ctx, item.TenantID, item.InboxID, item.AssignedTo); err != nil {
			return err
		}
		outcome = casepkg.OutcomeUnset
	default:
		return casepkg.Invalid("unsupported lifecycle action")
	}
	if err := casepkg.ValidateStatusTransition(item.Status, next); err != nil {
		return err
	}
	item.Status = next
	item.Outcome = outcome
	item.SnoozedUntil = nil
	item.BoostReason = nil
	return nil
}
