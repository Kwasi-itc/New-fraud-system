package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/google/uuid"
)

type TransferDecisionInput struct {
	TenantID    uuid.UUID `json:"tenant_id"`
	FromCaseID  uuid.UUID `json:"from_case_id"`
	ToCaseID    uuid.UUID `json:"to_case_id"`
	DecisionID  uuid.UUID `json:"decision_id"`
	OperationID uuid.UUID `json:"operation_id"`
	Reason      string    `json:"reason"`
}

func (s CaseService) TransferDecision(ctx context.Context, in TransferDecisionInput) error {
	p, err := access.Tenant(ctx, in.TenantID)
	if err != nil {
		return err
	}
	if p.Kind != access.User {
		return casepkg.ErrForbidden
	}
	if err := requireIDs(in.TenantID, in.FromCaseID, in.ToCaseID, in.DecisionID, in.OperationID); err != nil {
		return err
	}
	if in.FromCaseID == in.ToCaseID {
		return casepkg.Invalid("source and destination cases must differ")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || len(in.Reason) > 10000 {
		return casepkg.Invalid("transfer reason must be nonblank and at most 10000 bytes")
	}
	data, err := json.Marshal(struct {
		Input TransferDecisionInput
		Actor string
	}{in, p.Subject})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	_, err = transact(ctx, s, func(tx CaseService) (struct{}, error) {
		if err := tx.intake.Lock(ctx, in.TenantID, "decision.transfer", in.OperationID.String()); err != nil {
			return struct{}{}, err
		}
		ids := []uuid.UUID{in.FromCaseID, in.ToCaseID}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		cases := map[uuid.UUID]casepkg.Case{}
		for _, id := range ids {
			item, err := tx.caseForMutation(ctx, in.TenantID, id, access.Actor(ctx))
			if err != nil {
				return struct{}{}, err
			}
			cases[id] = item
		}
		// Authorization is rechecked even for a receipt replay.
		receipt, err := tx.intake.Get(ctx, in.TenantID, "decision.transfer", in.OperationID)
		if err != nil {
			return struct{}{}, err
		}
		if receipt != nil {
			if receipt.PayloadHash != hash {
				return struct{}{}, casepkg.ErrConflict
			}
			return struct{}{}, nil
		}
		source, target := cases[in.FromCaseID], cases[in.ToCaseID]
		if source.Type != casepkg.TypeDecision || target.Type != casepkg.TypeDecision {
			return struct{}{}, casepkg.Invalid("decision transfer requires decision cases")
		}
		if target.Status == casepkg.StatusClosed {
			return struct{}{}, casepkg.Invalid("destination case must be open")
		}
		if _, err := tx.activeInbox(ctx, in.TenantID, target.InboxID); err != nil {
			return struct{}{}, err
		}
		link, err := tx.decisions.Membership(ctx, in.TenantID, in.DecisionID)
		if err != nil {
			return struct{}{}, err
		}
		if link == nil || link.CaseID != in.FromCaseID {
			return struct{}{}, casepkg.ErrConflict
		}
		if err := tx.decisions.Transfer(ctx, in.TenantID, in.DecisionID, in.FromCaseID, in.ToCaseID); err != nil {
			return struct{}{}, err
		}
		now := tx.clock.Now()
		for _, id := range ids {
			kind := "decision_transferred_out"
			if id == in.ToCaseID {
				kind = "decision_transferred_in"
			}
			if _, err := tx.events.Create(ctx, newEvent(tx.ids.New(), in.TenantID, id, access.Actor(ctx), kind, in.Reason, in.DecisionID.String(), "decision", in.ToCaseID.String(), in.FromCaseID.String(), now)); err != nil {
				return struct{}{}, err
			}
			if err := tx.contributors.Add(ctx, in.TenantID, id, p.Subject, now); err != nil {
				return struct{}{}, err
			}
		}
		// Preserve source lifecycle/outcome; the destination uses existing human
		// investigation and capacity rules. Owner decision disposition is untouched.
		if err := tx.investigate(ctx, &target, true); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, tx.intake.Create(ctx, ports.IntakeReceipt{TenantID: in.TenantID, Source: "decision.transfer", EventID: in.OperationID, PayloadHash: hash, CaseID: in.ToCaseID, CreatedAt: now})
	})
	return err
}
