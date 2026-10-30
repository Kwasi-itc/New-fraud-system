package service

import (
	"context"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"time"
)

func (s ScreeningService) InvestigatorReview(ctx context.Context, store ports.MutationStore, tenant, resource, actor string, cmd *identity.Command) (identity.State, error) {
	match, err := store.ScreeningMatches().GetByID(ctx, tenant, resource)
	if err != nil {
		return identity.State{}, err
	}
	parent, err := store.Screenings().Lock(ctx, tenant, match.ScreeningID)
	if err != nil {
		return identity.State{}, err
	}
	match, err = store.ScreeningMatches().GetByID(ctx, tenant, resource)
	if err != nil {
		return identity.State{}, err
	}
	eligible := match.Status == "pending" && !parent.IsArchived &&
		(parent.Status == "awaiting_review" || parent.Status == "confirmed_hit")
	actions := []string{"confirmed_hit", "no_hit"}
	scope, scoped := objectExclusionScope(parent)
	excluded := false
	exclusionVersion := ""
	if scoped {
		continuous, err := store.Screenings().IsContinuousRun(ctx, tenant, parent.ID)
		if err != nil {
			return identity.State{}, err
		}
		scoped = continuous
	}
	if scoped {
		active, err := store.ObjectExclusions().Active(ctx, scope, []string{match.EntityID})
		if err != nil {
			return identity.State{}, err
		}
		exclusionVersion = active[match.EntityID]
		excluded = exclusionVersion != ""
		if eligible && !excluded {
			actions = append(actions, "dismiss_and_exclude")
		}
		// Removal changes future suppression, not the historical screening.
		// Case/inbox authority and lifecycle remain enforced by investigator.Run.
		if excluded && match.Status == "no_hit" {
			eligible = true
			actions = []string{"remove_exclusion"}
		}
	}
	if cmd != nil {
		if !eligible {
			return identity.State{}, identity.Conflict()
		}
		action, findings := cmd.Action, cmd.Findings
		if action == "dismiss_and_exclude" || action == "remove_exclusion" {
			allowed := false
			for _, value := range actions {
				if value == action {
					allowed = true
				}
			}
			if !scoped || !allowed {
				return identity.State{}, identity.Conflict()
			}
			active := action == "dismiss_and_exclude"
			change := screening.ExclusionChange{Scope: scope, EntityID: match.EntityID, Active: active, OperationID: cmd.OperationID.String(), ScreeningID: parent.ID, MatchID: match.ID, ActorID: actor, Findings: findings}
			if err := store.ObjectExclusions().Set(ctx, change); err != nil {
				if errors.Is(err, ports.ErrExclusionConflict) {
					return identity.State{}, identity.Conflict()
				}
				return identity.State{}, err
			}
			excluded = active
			exclusionVersion = cmd.OperationID.String()
			findings = "[" + action + "] " + findings
			action = "no_hit"
		}
		if cmd.Action != "remove_exclusion" {
			match, err = s.reviewMatch(ctx, store, tenant, resource, action, findings, actor, false, false)
			if err != nil {
				return identity.State{}, err
			}
		}
	}
	version := match.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if scoped {
		if excluded {
			version += ":" + exclusionVersion
		} else {
			version += ":included"
		}
	}
	return identity.State{Status: string(match.Status), Version: version, Eligible: eligible && cmd == nil, Actions: actions}, nil
}
