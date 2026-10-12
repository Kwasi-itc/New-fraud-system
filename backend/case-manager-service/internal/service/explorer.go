package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"time"
)

func (s CaseService) Explore(ctx context.Context, tenant, id uuid.UUID, in casepkg.ExplorationRequest) (casepkg.Exploration, error) {
	empty := casepkg.Exploration{}
	p, err := access.Tenant(ctx, tenant)
	if err != nil {
		return empty, err
	}
	if p.Kind != access.User {
		return empty, casepkg.ErrForbidden
	}
	if in.DecisionID == uuid.Nil || len(in.Path) > 6 || len(in.After) > 512 || len(in.Connection) > 80 {
		return empty, casepkg.Invalid("invalid exploration path")
	}
	for _, hop := range in.Path {
		if hop.Connection == "" || len(hop.Connection) > 80 || hop.ObjectID == "" || len(hop.ObjectID) > 512 {
			return empty, casepkg.Invalid("invalid exploration hop")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor, err := casepkg.ParseCursor(in.DecisionAfter)
	if err != nil {
		return empty, err
	}
	return transact(ctx, s, func(tx CaseService) (casepkg.Exploration, error) {
		item, err := tx.workspaceCase(ctx, tenant, id)
		if err != nil {
			return empty, err
		}
		if err := tx.EvidenceAccess(ctx, tenant, id, "decisions", in.DecisionID); err != nil {
			return empty, err
		}
		refs, err := tx.references.Decisions(ctx, tenant, []uuid.UUID{in.DecisionID})
		if err != nil {
			return empty, err
		}
		if tx.explorer == nil {
			return empty, casepkg.Invalid("record exploration unavailable")
		}
		out, err := tx.explorer.Explore(ctx, tenant, refs[in.DecisionID], in)
		if err != nil {
			return empty, err
		}
		out.Decisions, err = tx.explorer.Decisions(ctx, tenant, out.Table, out.Current.ObjectID, p.Subject, p.Admin, cursor)
		if err != nil {
			return empty, err
		}
		if len(out.Decisions) > 25 {
			out.Decisions = out.Decisions[:25]
			last := out.Decisions[24]
			out.NextDecision = (casepkg.Cursor{ID: last.ID, At: last.CreatedAt}).Encode()
		}
		for n := range out.Decisions {
			out.Decisions[n].CanAttach = out.Decisions[n].Case == nil && item.Status != casepkg.StatusClosed && item.Type == casepkg.TypeDecision
		}
		return out, nil
	})
}

func (s CaseService) AttachExploredDecision(ctx context.Context, tenant, id uuid.UUID, in casepkg.ExplorationAttach) (casepkg.DecisionLink, error) {
	empty := casepkg.DecisionLink{}
	p, err := access.Tenant(ctx, tenant)
	if err != nil {
		return empty, err
	}
	if p.Kind != access.User {
		return empty, casepkg.ErrForbidden
	}
	if in.TargetDecisionID == uuid.Nil {
		return empty, casepkg.Invalid("target decision is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transact(ctx, s, func(tx CaseService) (casepkg.DecisionLink, error) {
		// Acquire the mutation lock before path reads, avoiding shared-to-exclusive
		// lock upgrades when two investigators attach to the same case concurrently.
		if _, err := tx.caseForMutation(ctx, tenant, id, access.Actor(ctx)); err != nil {
			return empty, err
		}
		in.Connection = ""
		in.After = ""
		in.DecisionAfter = ""
		record, err := tx.Explore(ctx, tenant, id, in.ExplorationRequest)
		if err != nil {
			return empty, err
		}
		refs, err := tx.references.Decisions(ctx, tenant, []uuid.UUID{in.TargetDecisionID})
		if err != nil {
			return empty, err
		}
		target := refs[in.TargetDecisionID]
		if target.ObjectType != record.Table || target.ObjectID != record.Current.ObjectID {
			return empty, casepkg.ErrNotFound
		}
		// Reuse the existing single-primary membership, replay, lifecycle, assignment,
		// capacity and audit rules. A concurrent attachment elsewhere remains a conflict.
		return tx.addDecision(ctx, AddDecisionInput{TenantID: tenant, CaseID: id, DecisionID: target.ID}, access.Actor(ctx))
	})
}
