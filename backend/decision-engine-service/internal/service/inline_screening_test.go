package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/localmatching"
	"github.com/google/uuid"
)

type inlineIDs struct{}

func (inlineIDs) New() uuid.UUID { return uuid.New() }

func inlineEvaluator(ctx context.Context, _ string, request json.RawMessage) (screening.InlineEvaluation, error) {
	result, count, partial, err := localmatching.Evaluate(ctx, request)
	return screening.InlineEvaluation{Result: result, MatchCount: count, Partial: partial}, err
}

func TestInlineScreeningOutcomesAndEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, forced, query string
		initial, want       decision.Outcome
	}{
		{"review hit", "review", "Alex Example", decision.OutcomeApprove, decision.OutcomeReview},
		{"block hit", "block_and_review", "Alex Example", decision.OutcomeApprove, decision.OutcomeBlockAndReview},
		{"decline hit", "decline", "Alex Example", decision.OutcomeReview, decision.OutcomeDecline},
		{"no hit", "decline", "Unrelated synthetic entity", decision.OutcomeApprove, decision.OutcomeApprove},
		{"never downgrade", "review", "Alex Example", decision.OutcomeDecline, decision.OutcomeDecline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := screening.AuthoringRule{EntityType: "Person", NameField: "name", ForcedOutcome: tc.forced}
			svc := DecisionService{clock: fixedClock{}, idGen: inlineIDs{}, InlineScreeningEvaluator: inlineEvaluator}
			item := decision.Decision{ID: uuid.NewString(), TenantID: uuid.NewString(), ScenarioID: uuid.NewString(), Outcome: tc.initial, Score: 42}
			routing := &ports.EvaluationRoutingSnapshot{ScreeningConfigs: []screening.Config{{ID: uuid.NewString(), Provider: "opensanctions", ConfigJSON: rule.ConfigJSON(), AllowedOutcomes: []string{"review"}}}}
			executions, err := svc.applyInlineScreening(context.Background(), &item, map[string]any{"name": tc.query}, routing)
			if err != nil || item.Outcome != tc.want || item.Score != 42 || len(executions) != 1 {
				t.Fatal(item, executions, err)
			}
			var captured struct {
				InlineResult json.RawMessage `json:"inline_result"`
			}
			if json.Unmarshal(executions[0].RequestJSON, &captured) != nil || len(captured.InlineResult) == 0 {
				t.Fatal("missing durable evidence")
			}
			async, err := svc.buildScreeningExecutionsWithRouting(context.Background(), item, map[string]any{"name": tc.query}, routing)
			if err != nil || len(async) != 0 {
				t.Fatal("inline query duplicated by async phase", err)
			}
		})
	}
}

func TestInlineScreeningMultipleRulesConditionAndDeadline(t *testing.T) {
	item := decision.Decision{ID: uuid.NewString(), TenantID: uuid.NewString(), ScenarioID: uuid.NewString(), Outcome: decision.OutcomeApprove}
	routing := &ports.EvaluationRoutingSnapshot{}
	for _, forced := range []string{"decline", "review", "block_and_review"} {
		rule := screening.AuthoringRule{EntityType: "Person", NameField: "name", ForcedOutcome: forced}
		routing.ScreeningConfigs = append(routing.ScreeningConfigs, screening.Config{ID: uuid.NewString(), Provider: "opensanctions", ConfigJSON: rule.ConfigJSON()})
	}
	calls := 0
	svc := DecisionService{clock: fixedClock{}, idGen: inlineIDs{}, InlineScreeningEvaluator: func(ctx context.Context, tenant string, request json.RawMessage) (screening.InlineEvaluation, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > inlineScreeningTimeout {
			t.Fatal("missing bounded provider deadline")
		}
		return inlineEvaluator(ctx, tenant, request)
	}}
	if _, err := svc.applyInlineScreening(context.Background(), &item, map[string]any{"name": "Alex Example"}, routing); err != nil || item.Outcome != decision.OutcomeDecline || calls != 3 {
		t.Fatal(item, calls, err)
	}
	rule := screening.AuthoringRule{EntityType: "Person", NameField: "name", ForcedOutcome: "review", Condition: &workflow.ConditionTree{Kind: "payload", Field: "eligible", Operator: "exists"}}
	routing.ScreeningConfigs = []screening.Config{{ID: uuid.NewString(), Provider: "opensanctions", ConfigJSON: rule.ConfigJSON()}}
	calls = 0
	if values, err := svc.applyInlineScreening(context.Background(), &item, map[string]any{"name": "Alex Example"}, routing); err != nil || len(values) != 0 || calls != 0 {
		t.Fatal("false condition searched", err)
	}
}

type inlineConfigRepo struct {
	ports.ScreeningConfigRepository
	config screening.Config
}

func (r inlineConfigRepo) ListActiveByScenario(context.Context, string, string) ([]screening.Config, error) {
	return []screening.Config{r.config}, nil
}

type inlineWriteSpy struct{ called bool }

func (s *inlineWriteSpy) Run(context.Context, func(ports.MutationStore) error) error {
	s.called = true
	return errors.New("unexpected write")
}

func TestInlineFailureAndTimeoutNeverPersistADecision(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "timeout"}[timeout], func(t *testing.T) {
			writes := &inlineWriteSpy{}
			cfg := screening.Config{ID: uuid.NewString(), Provider: "opensanctions", ConfigJSON: json.RawMessage(`{"entity_type":"Person","query":{"name":"Alex Example"},"forced_outcome":"decline"}`)}
			svc := newFailureTestDecisionService(writes, nilScoringConfigRepository(nil), nilScoringRequestRepository(nil), inlineConfigRepo{config: cfg})
			svc.InlineScreeningEvaluator = func(ctx context.Context, _ string, _ json.RawMessage) (screening.InlineEvaluation, error) {
				if timeout {
					<-ctx.Done()
				}
				return screening.InlineEvaluation{}, errors.New("provider failed")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			result, err := svc.EvaluateScenario(ctx, "tenant-1", "scenario-1", failureTestEvaluationRequest())
			if !errors.Is(err, screening.ErrInlineFailed) || writes.called || result.Decision != nil {
				t.Fatal("failure produced a decision or writes", result, writes.called, err)
			}
		})
	}
}
