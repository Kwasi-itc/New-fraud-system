package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type baselineDecisionRepo struct {
	stubDecisionRepo
	stored decision.Decision
}

func (r *baselineDecisionRepo) Create(_ context.Context, d decision.Decision) (decision.Decision, error) {
	r.stored = d
	return d, nil
}

type baselineEvidenceRepo struct {
	nilRuleExecutionRepository
	stored []decision.RuleExecution
}

func (r *baselineEvidenceRepo) CreateMany(_ context.Context, items []decision.RuleExecution) ([]decision.RuleExecution, error) {
	r.stored = items
	return items, nil
}

// This corpus freezes actual evaluator outcomes, evidence and persistence input.
// It is the PostgreSQL-independent baseline for a later MySQL differential run,
// not proof that the not-yet-written MySQL repositories produce the same result.
func TestDecisionMigrationBaselineOutcomesAndEvidence(t *testing.T) {
	for _, test := range []struct {
		name  string
		score int
		hit   bool
		want  decision.Outcome
	}{
		{"below_review", 9, true, decision.OutcomeApprove}, {"review_edge", 10, true, decision.OutcomeReview}, {"block_edge", 20, true, decision.OutcomeBlockAndReview}, {"decline_edge", 30, true, decision.OutcomeDecline}, {"no_hit", 100, false, decision.OutcomeApprove},
	} {
		t.Run(test.name, func(t *testing.T) {
			decisions, evidence := &baselineDecisionRepo{}, &baselineEvidenceRepo{}
			store := decisionMutationStoreStub{decisionRepo: decisions, ruleExecutionRepo: evidence, workflowExecutionRepo: workflowExecutionRepoStub{}, screeningExecutionRepo: nilScreeningExecutionRepository(nil), scoringRequestRepo: nilScoringRequestRepository(nil)}
			svc := newFailureTestDecisionService(txManagerStub{store: store}, nilScoringConfigRepository(nil), nilScoringRequestRepository(nil), nilScreeningConfigRepository(nil))
			formula := json.RawMessage(`{"constant":false}`)
			if test.hit {
				formula = json.RawMessage(`{"constant":true}`)
			}
			svc.ruleRepo = ruleRepoStub{rules: []scenario.Rule{{ID: "rule-1", IterationID: "iter-1", TenantID: "tenant-1", Name: "baseline", ScoreModifier: test.score, Formula: formula}}}
			result, err := svc.EvaluateScenario(context.Background(), "tenant-1", "scenario-1", failureTestEvaluationRequest())
			if err != nil {
				t.Fatal(err)
			}
			score := 0
			if test.hit {
				score = test.score
			}
			d := decisions.stored
			if result.Decision == nil || d.ID == "" || d.TenantID != "tenant-1" || d.ScenarioID != "scenario-1" || d.ScenarioIterationID != "iter-1" || d.ObjectID != "txn-1" || d.ObjectType != "transactions" || d.Outcome != test.want || d.Score != score || !d.Triggered || d.CreatedAt.IsZero() {
				t.Fatalf("decision baseline drift: %+v", d)
			}
			if result.Decision.ID != d.ID || result.Decision.Score != score || result.Decision.Outcome != test.want {
				t.Fatalf("response/persistence differ: %+v", result)
			}
			var body map[string]any
			if err := json.Unmarshal(d.RequestBody, &body); err != nil || len(body) == 0 {
				t.Fatalf("request evidence missing: %s %v", d.RequestBody, err)
			}
			if len(evidence.stored) != 1 {
				t.Fatalf("rule evidence count=%d", len(evidence.stored))
			}
			e := evidence.stored[0]
			want := "no_hit"
			if test.hit {
				want = "hit"
			}
			if e.RuleID != "rule-1" || e.DecisionID != d.ID || e.Outcome != want || e.ScoreModifier != test.score || e.Evaluation == nil || e.Evaluation.ReturnValue != test.hit {
				t.Fatalf("rule evidence baseline drift: %+v", e)
			}
		})
	}
}
