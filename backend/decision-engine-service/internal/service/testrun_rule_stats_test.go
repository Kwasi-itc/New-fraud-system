package service

import (
	"context"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
)

type phantomDecisionStatsStub []decision.PhantomDecision

func (s phantomDecisionStatsStub) Create(_ context.Context, item decision.PhantomDecision) (decision.PhantomDecision, error) {
	return item, nil
}

func (s phantomDecisionStatsStub) ListByTestRun(context.Context, string, string) ([]decision.PhantomDecision, error) {
	return s, nil
}

type phantomRuleExecutionStatsStub map[string][]decision.PhantomRuleExecution

func (s phantomRuleExecutionStatsStub) CreateMany(_ context.Context, items []decision.PhantomRuleExecution) ([]decision.PhantomRuleExecution, error) {
	return items, nil
}

func (s phantomRuleExecutionStatsStub) ListByPhantomDecision(_ context.Context, _, phantomDecisionID string) ([]decision.PhantomRuleExecution, error) {
	return s[phantomDecisionID], nil
}

func TestRuleStatsCountsSkippedSeparatelyFromNoHit(t *testing.T) {
	t.Parallel()

	service := TestRunService{
		phantomDecisionRepo: phantomDecisionStatsStub{{ID: "phantom-1"}},
		phantomRuleExecRepo: phantomRuleExecutionStatsStub{
			"phantom-1": {
				{RuleID: "rule-1", RuleName: "baseline", Outcome: "hit"},
				{RuleID: "rule-1", RuleName: "baseline", Outcome: "no_hit"},
				{RuleID: "rule-1", RuleName: "baseline", Outcome: "snoozed"},
				{RuleID: "rule-1", RuleName: "baseline", Outcome: "skipped", SkipReason: "multiply left operand is null"},
			},
		},
	}

	stats, err := service.RuleStats(context.Background(), "tenant-1", "run-1")
	if err != nil {
		t.Fatalf("RuleStats() error = %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("RuleStats() returned %d items, want 1", len(stats))
	}
	if stats[0].HitCount != 1 || stats[0].NoHitCount != 1 ||
		stats[0].SnoozedCount != 1 || stats[0].SkippedCount != 1 || stats[0].TotalCount != 4 {
		t.Fatalf("unexpected stats: %#v", stats[0])
	}
}
