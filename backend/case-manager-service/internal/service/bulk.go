package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/observability"
	"log/slog"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type BulkInput struct {
	OperationID uuid.UUID       `json:"operation_id"`
	CaseIDs     []uuid.UUID     `json:"case_ids"`
	Action      string          `json:"action"`
	Assignee    *string         `json:"assignee"`
	InboxID     *uuid.UUID      `json:"inbox_id"`
	Outcome     casepkg.Outcome `json:"outcome"`
	Comment     string          `json:"comment"`
}
type BulkResult struct {
	CaseID   uuid.UUID `json:"case_id"`
	Success  bool      `json:"success"`
	Replayed bool      `json:"replayed"`
	Error    string    `json:"error,omitempty"`
}

// Each case commits independently. The same operation ID/payload safely resumes
// after disconnects; failures leave no receipt and can be retried.
func (s CaseService) Bulk(ctx context.Context, tenant uuid.UUID, in BulkInput) ([]BulkResult, error) {
	if _, err := access.Tenant(ctx, tenant); err != nil {
		return nil, err
	}
	if access.Actor(ctx) == nil {
		return nil, casepkg.ErrForbidden
	}
	if err := requireIDs(tenant, in.OperationID); err != nil {
		return nil, err
	}
	if len(in.CaseIDs) < 1 || len(in.CaseIDs) > 100 {
		return nil, casepkg.Invalid("select 1..100 cases")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range in.CaseIDs {
		if id == uuid.Nil || seen[id] {
			return nil, casepkg.Invalid("case IDs must be unique and nonzero")
		}
		seen[id] = true
	}
	switch in.Action {
	case "close":
		if err := validateClosingFindings(in.Outcome, in.Comment); err != nil {
			return nil, err
		}
	case "reopen":
		if in.Outcome != "" && in.Outcome != casepkg.OutcomeUnset {
			return nil, casepkg.Invalid("reopening resets outcome to unset")
		}
	case "assign":
		if in.Assignee != nil && strings.TrimSpace(*in.Assignee) == "" {
			return nil, casepkg.Invalid("assignee cannot be empty")
		}
	case "move":
		if in.InboxID == nil || *in.InboxID == uuid.Nil {
			return nil, casepkg.Invalid("destination inbox required")
		}
	default:
		return nil, casepkg.Invalid("unsupported bulk action")
	}
	results := make([]BulkResult, 0, len(in.CaseIDs))
	for _, id := range in.CaseIDs {
		replayed, err := s.bulkOne(ctx, tenant, id, in)
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}
		if bulkError(err) == "internal_error" {
			level = slog.LevelError
		}
		observability.Logger(ctx).Log(ctx, level, "bulk case item completed", append([]any{"event", "case.bulk.item", "tenant_id", tenant, "case_id", id, "operation_id", in.OperationID, "action", in.Action, "success", err == nil, "replayed", replayed}, observability.ErrorFields(err)...)...)
		results = append(results, BulkResult{CaseID: id, Success: err == nil, Replayed: replayed, Error: bulkError(err)})
	}
	return results, nil
}

func (s CaseService) bulkOne(ctx context.Context, tenant, id uuid.UUID, in BulkInput) (bool, error) {
	actor := access.Actor(ctx)
	payload := in
	payload.CaseIDs = nil
	raw, err := json.Marshal(struct {
		Actor string
		Input BulkInput
	}{*actor, payload})
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	receiptID := uuid.NewSHA1(in.OperationID, id[:])
	return transact(ctx, s, func(tx CaseService) (bool, error) {
		var destinations []uuid.UUID
		if in.Action == "move" {
			destinations = append(destinations, *in.InboxID)
		}
		item, err := tx.caseForMutation(ctx, tenant, id, actor, destinations...)
		if err != nil {
			return false, err
		}
		receipt, err := tx.intake.Get(ctx, tenant, "bulk", receiptID)
		if err != nil {
			return false, err
		}
		if receipt != nil {
			if receipt.PayloadHash != hash {
				return false, casepkg.ErrConflict
			}
			return true, nil
		}
		before := item
		now := tx.clock.Now()
		switch in.Action {
		case "close":
			if err := tx.applyLifecycle(ctx, &item, casepkg.StatusClosed, in.Outcome, in.Comment); err != nil {
				return false, err
			}
		case "reopen":
			if err := tx.applyLifecycle(ctx, &item, casepkg.StatusInvestigating, casepkg.OutcomeUnset, ""); err != nil {
				return false, err
			}
		case "assign":
			if item.Status == casepkg.StatusClosed {
				return false, casepkg.ErrConflict
			}
			if err := tx.eligibleAssignee(ctx, tenant, item.InboxID, in.Assignee); err != nil {
				return false, err
			}
			item.AssignedTo = in.Assignee
		case "move":
			if _, err := tx.activeInbox(ctx, tenant, *in.InboxID); err != nil {
				return false, err
			}
			if err := tx.inboxAccess(ctx, tenant, *in.InboxID); err != nil {
				return false, err
			}
			if err := tx.eligibleAssignee(ctx, tenant, *in.InboxID, item.AssignedTo); err != nil {
				return false, err
			}
			item.InboxID = *in.InboxID
		}
		item.UpdatedAt = now
		if _, err := tx.cases.Update(ctx, item); err != nil {
			return false, err
		}
		if err := tx.contributors.Add(ctx, tenant, id, *actor, now); err != nil {
			return false, err
		}
		oldJSON, err := json.Marshal(before)
		if err != nil {
			return false, err
		}
		newJSON, err := json.Marshal(item)
		if err != nil {
			return false, err
		}
		_, err = tx.events.Create(ctx, newEvent(tx.ids.New(), tenant, id, actor, "bulk_"+in.Action, strings.TrimSpace(in.Comment), in.OperationID.String(), "bulk_operation", string(newJSON), string(oldJSON), now))
		if err != nil {
			return false, err
		}
		err = tx.intake.Create(ctx, ports.IntakeReceipt{TenantID: tenant, Source: "bulk", EventID: receiptID, PayloadHash: hash, CaseID: id, CreatedAt: now})
		return false, err
	})
}

func bulkError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, casepkg.ErrForbidden):
		return "forbidden"
	case errors.Is(err, casepkg.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		return "not_found"
	case errors.Is(err, casepkg.ErrValidation):
		return "invalid_state_or_reference"
	case errors.Is(err, casepkg.ErrConflict):
		return "conflict"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "interrupted_retry_with_same_operation_id"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505", "40001", "40P01", "55P03", "23514":
			return "conflict"
		}
	}
	return "internal_error"
}
