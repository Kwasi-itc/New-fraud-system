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
	if parent.Partial {
		return identity.State{Status: string(match.Status), Version: match.UpdatedAt.UTC().Format(time.RFC3339Nano), Eligible: false, Actions: []string{}, Reason: "Screening results are incomplete. Refine the search before reviewing matches."}, nil
	}
	if native, ok := store.(interface {
		IsScreeningSuperseded(context.Context, string, string) (bool, error)
	}); ok {
		replaced, e := native.IsScreeningSuperseded(ctx, tenant, parent.ID)
		if e != nil {
			return identity.State{}, e
		}
		if replaced {
			return identity.State{Status: string(match.Status), Version: match.UpdatedAt.UTC().Format(time.RFC3339Nano), Eligible: false, Actions: []string{}, Reason: "This screening was refined. Review its replacement; the original evidence is retained."}, nil
		}
	}
	actions := []string{"confirmed_hit", "no_hit"}
	whitelistScope := parent
	if native, ok := store.(interface {
		CounterpartyScope(context.Context, screening.Screening) (screening.Screening, error)
	}); ok {
		whitelistScope, err = native.CounterpartyScope(ctx, parent)
		if err != nil {
			return identity.State{}, err
		}
	}
	if native, ok := store.(ports.CounterpartyExclusionStore); ok && whitelistScope.UniqueCounterpartyIdentifier != nil && *whitelistScope.UniqueCounterpartyIdentifier != "" && !whitelistScope.IsManual && whitelistScope.DecisionID != "" {
		active, e := native.CounterpartyExclusions(ctx, tenant, whitelistScope.Provider, *whitelistScope.UniqueCounterpartyIdentifier, []string{match.EntityID})
		if e != nil {
			return identity.State{}, e
		}
		if eligible {
			actions = append(actions, "dismiss_and_whitelist")
		}
		if active[match.EntityID] != "" && match.Status == "no_hit" {
			eligible = true
			actions = []string{"remove_counterparty_exclusion"}
		}
		if cmd != nil && (cmd.Action == "dismiss_and_whitelist" || cmd.Action == "remove_counterparty_exclusion") {
			if !eligible {
				return identity.State{}, identity.Conflict()
			}
			allowed := false
			for _, a := range actions {
				allowed = allowed || a == cmd.Action
			}
			if !allowed {
				return identity.State{}, identity.Conflict()
			}
			if e := native.SetCounterpartyExclusion(ctx, whitelistScope, match, actor, cmd, cmd.Action == "dismiss_and_whitelist"); e != nil {
				return identity.State{}, e
			}
			if cmd.Action == "dismiss_and_whitelist" {
				match, e = s.reviewMatch(ctx, store, tenant, resource, "no_hit", "[counterparty whitelist] "+cmd.Findings, actor, false, false)
				if e != nil {
					return identity.State{}, e
				}
			}
			return identity.State{Status: string(match.Status), Version: match.UpdatedAt.UTC().Format(time.RFC3339Nano), Eligible: false, Actions: []string{}}, nil
		}
	}
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
