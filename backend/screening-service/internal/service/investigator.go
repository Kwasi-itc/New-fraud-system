package service

import (
	"context"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"time"
)

func (s ScreeningService) InvestigatorReview(ctx context.Context, store ports.MutationStore, tenant, resource, actor string, cmd *identity.Command) (identity.State, error) {
	match, err := store.ScreeningMatches().GetByID(ctx, tenant, resource)
	if err != nil {
		return identity.State{}, err
	}
	if _, err = store.Screenings().Lock(ctx, tenant, match.ScreeningID); err != nil {
		return identity.State{}, err
	}
	match, err = store.ScreeningMatches().GetByID(ctx, tenant, resource)
	if err != nil {
		return identity.State{}, err
	}
	if cmd != nil {
		match, err = s.reviewMatch(ctx, store, tenant, resource, cmd.Action, cmd.Findings, actor, false, false)
		if err != nil {
			return identity.State{}, err
		}
	}
	return identity.State{Status: string(match.Status), Version: match.UpdatedAt.UTC().Format(time.RFC3339Nano), Eligible: match.Status == "pending", Actions: []string{"confirmed_hit", "no_hit"}}, nil
}
