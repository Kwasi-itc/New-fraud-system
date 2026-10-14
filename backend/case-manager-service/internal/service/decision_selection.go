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
	"sort"
	"strings"
)

func (s CaseService) DecisionSelections(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID) ([]casepkg.DecisionSelection, error) {
	p, err := access.Tenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if p.Kind != access.User {
		return nil, casepkg.ErrForbidden
	}
	if len(ids) == 0 || len(ids) > 100 {
		return nil, casepkg.Invalid("select between 1 and 100 decisions")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			return nil, casepkg.Invalid("decision IDs must be unique nonzero UUIDs")
		}
		seen[id] = true
	}
	return transact(ctx, s, func(tx CaseService) ([]casepkg.DecisionSelection, error) {
		return tx.selections.Lookup(ctx, tenant, ids, p.Subject, p.Admin)
	})
}

// Optional for legacy callers. New UI requests bind an immutable receipt to the
// verified actor and normalized payload, committing it with the complete case.
func (s CaseService) createCaseWithReceipt(ctx context.Context, in CreateCaseInput, actor *string) (casepkg.Case, error) {
	if in.OperationID == nil {
		return s.createCase(ctx, in, actor)
	}
	if actor == nil {
		return casepkg.Case{}, casepkg.ErrForbidden
	}
	if *in.OperationID == uuid.Nil || len(in.DecisionIDs) > 100 {
		return casepkg.Case{}, casepkg.Invalid("invalid operation ID or more than 100 decisions")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Type == "" {
		in.Type = casepkg.TypeDecision
	}
	in.DecisionIDs = append([]uuid.UUID{}, in.DecisionIDs...)
	sort.Slice(in.DecisionIDs, func(i, j int) bool { return in.DecisionIDs[i].String() < in.DecisionIDs[j].String() })
	data, err := json.Marshal(struct {
		Input CreateCaseInput
		Actor string
	}{in, *actor})
	if err != nil {
		return casepkg.Case{}, err
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	const source = "case.manual-create"
	if err := s.intake.Lock(ctx, in.TenantID, source, in.OperationID.String()); err != nil {
		return casepkg.Case{}, err
	}
	receipt, err := s.intake.Get(ctx, in.TenantID, source, *in.OperationID)
	if err != nil {
		return casepkg.Case{}, err
	}
	if receipt != nil {
		if receipt.PayloadHash != hash {
			return casepkg.Case{}, casepkg.ErrConflict
		}
		return s.getCase(ctx, in.TenantID, receipt.CaseID)
	}
	item, err := s.createCase(ctx, in, actor)
	if err != nil {
		return casepkg.Case{}, err
	}
	err = s.intake.Create(ctx, ports.IntakeReceipt{TenantID: in.TenantID, Source: source, EventID: *in.OperationID, PayloadHash: hash, CaseID: item.ID, CreatedAt: s.clock.Now()})
	return item, err
}
