package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/lookup"
	"github.com/jackc/pgx/v5"
)

type InlineScreening interface {
	Evaluate(context.Context, lookup.Input) (*lookup.Result, error)
	Persist(context.Context, pgx.Tx, *lookup.Result) error
}
type InlinePolicy struct {
	Enabled bool
	Failure string
	Timeout time.Duration
}

func (p InlinePolicy) Validate() error {
	if !p.Enabled {
		return nil
	}
	if p.Failure != "error" || p.Timeout < 10*time.Millisecond || p.Timeout > 5*time.Second {
		return fmt.Errorf("inline screening requires the approved error policy and timeout from 10ms to 5s")
	}
	return nil
}

var ErrInlineEvaluation = errors.New("decision-time screening did not complete; no successful decision was created")

type InlineFailure struct{ Status string }

func (e *InlineFailure) Error() string {
	return "decision-time screening " + e.Status + "; no successful decision or effects were saved"
}
func (e *InlineFailure) Unwrap() error { return ErrInlineEvaluation }

func higherOutcome(current decision.Outcome, requested string) decision.Outcome {
	rank := map[decision.Outcome]int{decision.OutcomeApprove: 0, decision.OutcomeReview: 1, decision.OutcomeBlockAndReview: 2, decision.OutcomeDecline: 3}
	candidate := decision.Outcome(requested)
	if score, ok := rank[candidate]; ok && score > rank[current] {
		return candidate
	}
	return current
}

func (s DecisionService) evaluateInlineScreenings(ctx context.Context, item decision.Decision, fields map[string]any) (decision.Decision, []screening.Execution, []*lookup.Result, error) {
	execs, err := s.buildSelectedScreenings(ctx, item, fields, true)
	if err != nil {
		return item, nil, nil, err
	}
	if len(execs) == 0 {
		return item, nil, nil, nil
	}
	if !s.InlinePolicy.Enabled || s.InlinePolicy.Validate() != nil || s.InlineScreening == nil {
		return item, nil, nil, screening.ErrInlineUnavailable
	}
	configs, err := s.getActiveScreeningConfigs(ctx, item.TenantID, item.ScenarioID)
	if err != nil {
		return item, nil, nil, err
	}
	if len(execs) > 10 {
		return item, nil, nil, ErrInlineEvaluation
	}
	budget, cancel := context.WithTimeout(ctx, s.InlinePolicy.Timeout)
	defer cancel()
	byID := map[string]screening.Config{}
	for _, c := range configs {
		byID[c.ID] = c
	}
	original := string(item.Outcome)
	results := make([]*lookup.Result, 0, len(execs))
	for i := range execs {
		cfg := byID[execs[i].ConfigID]
		rule, ok := screening.DecodeAuthoring(cfg)
		if !ok || cfg.Provider != "internal-watchlists" {
			return item, nil, nil, screening.ErrInlineUnavailable
		}
		policy := lookup.Policy{Version: 1, Failure: s.InlinePolicy.Failure, TimeoutMS: s.InlinePolicy.Timeout.Milliseconds(), ForcedOutcome: rule.ForcedOutcome, OriginalOutcome: original}
		result, lookupErr := s.InlineScreening.Evaluate(budget, lookup.Input{TenantID: item.TenantID, ScenarioID: item.ScenarioID, ConfigID: cfg.ID, DecisionID: item.ID, ExecutionID: execs[i].ID, Config: cfg.ConfigJSON, Request: execs[i].RequestJSON, Policy: policy})
		if errors.Is(lookupErr, lookup.ErrAuthority) || result == nil {
			return item, nil, nil, &InlineFailure{Status: "configuration_changed"}
		}
		complete := !result.Partial && ((result.Status == "no_candidates" && result.MatchCount == 0) || (result.Status == "candidates" && result.MatchCount > 0))
		failed := lookupErr != nil || !complete
		if failed {
			status := result.Status
			if status != "timed_out" && status != "incomplete" {
				status = "failed"
			}
			return item, nil, nil, &InlineFailure{Status: status}
		} else if result.MatchCount > 0 {
			item.Outcome = higherOutcome(item.Outcome, rule.ForcedOutcome)
		}
		now := s.clock.Now()
		execs[i].Status = screening.ExecutionStatusCompleted
		execs[i].CompletedAt = &now
		execs[i].SentAt = &now
		execs[i].ProviderReference = result.Reference
		execs[i].ResponseJSON, _ = json.Marshal(map[string]any{"lookup_status": result.Status, "partial": result.Partial, "match_count": result.MatchCount, "execution_mode": "inline"})
		results = append(results, result)
	}
	for _, r := range results {
		r.Policy.FinalOutcome = string(item.Outcome)
	}
	return item, execs, results, nil
}
