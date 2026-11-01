package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestExecutionBatchDetachedExactAndAllOrNothing(t *testing.T) {
	ctx := context.Background()
	model := preparationModel()
	limits := ExecutionPreparationLimits{10, 1 << 20}
	items := []DecisionEvaluationRequest{{ObjectID: " ID ", ObjectType: "transactions", Fields: map[string]any{"amount": json.Number("12.5")}}}
	batch, err := PrepareObjectBatch(ctx, "tenant-1", "transactions", items, model, nil, limits)
	if err != nil {
		t.Fatal(err)
	}
	items[0].Fields["amount"] = 999
	table := model.Tables["transactions"]
	table.Fields["amount"] = ports.TenantModelField{Type: "bool"}
	model.Tables["transactions"] = table
	if batch.items[0].ObjectID != " ID " || batch.items[0].Fields["amount"] != float64(12.5) || batch.model.Tables["transactions"].Fields["amount"].Type != "float" {
		t.Fatal("input/model alias or identity changed", batch)
	}
	copy, err := batch.Model()
	if err != nil {
		t.Fatal(err)
	}
	delete(copy.Tables, "transactions")
	if len(batch.model.Tables) != 1 {
		t.Fatal("model accessor leaked mutable authority")
	}
	bad := []DecisionEvaluationRequest{items[0], {ObjectID: "second", ObjectType: "transactions", Fields: map[string]any{"amount": "not a number"}}}
	if out, err := PrepareObjectBatch(ctx, "tenant-1", "transactions", bad, preparationModel(), nil, limits); err == nil || out.Len() != 0 {
		t.Fatal("partial successful batch", out, err)
	}
	if _, err := PrepareObjectBatch(ctx, "tenant-1", "wallets", items, preparationModel(), nil, limits); err == nil {
		t.Fatal("mixed object types accepted")
	}
	if _, err := PrepareObjectBatch(ctx, "tenant-1", "transactions", append(items, items...), preparationModel(), nil, ExecutionPreparationLimits{1, 1 << 20}); !errors.Is(err, ErrExecutionPreparationLimit) {
		t.Fatal("count bound", err)
	}
	cycle := map[string]any{}
	cycle["cycle"] = cycle
	remaining, nodes := 1<<20, 0
	if err := boundExecutionValue(cycle, &remaining, &nodes, 0); err == nil {
		t.Fatal("cyclic/unbounded request accepted")
	}
	large := []DecisionEvaluationRequest{{ObjectID: "one", ObjectType: "transactions", Fields: map[string]any{"amount": 1, "note": strings.Repeat("x", 2000)}}}
	if _, err := PrepareObjectBatch(ctx, "tenant-1", "transactions", large, preparationModel(), nil, ExecutionPreparationLimits{20, 1024}); !errors.Is(err, ErrExecutionPreparationLimit) {
		t.Fatal("oversized item accepted", err)
	}
	many := make([]DecisionEvaluationRequest, 10)
	for i := range many {
		many[i] = DecisionEvaluationRequest{ObjectID: "repeated", ObjectType: "transactions", Fields: map[string]any{"amount": 1, "note": strings.Repeat("x", 100)}}
	}
	if _, err := PrepareObjectBatch(ctx, "tenant-1", "transactions", many, preparationModel(), nil, ExecutionPreparationLimits{20, 1024}); !errors.Is(err, ErrExecutionPreparationLimit) {
		t.Fatal("batch byte budget ignored", err)
	}
	if _, err := FreshExecutionModel(ctx, "tenant-1", nil, 1<<20); !errors.Is(err, ErrExecutionPreparation) {
		t.Fatal("missing model provider accepted", err)
	}
	if _, err := PrepareObjectBatch(ctx, "tenant-1", "transactions", []DecisionEvaluationRequest{{ObjectID: "absent", ObjectType: "transactions"}}, preparationModel(), nil, limits); err == nil {
		t.Fatal("missing stored reader accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := PrepareObjectBatch(cancelled, "tenant-1", "transactions", items, preparationModel(), nil, limits); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	// Duplicate object IDs are source-compatible independent input items, not
	// silently deduplicated or newly prohibited by the target preparation code.
	if out, err := PrepareObjectBatch(ctx, "tenant-1", "transactions", append(items, items...), preparationModel(), nil, limits); err != nil || out.Len() != 2 {
		t.Fatal("source batch semantics changed", out, err)
	}
}

func preparedPinFixture(t *testing.T, formula json.RawMessage) (PinnedScenarioSnapshot, PreparedObjectBatch) {
	t.Helper()
	at := time.Now().UTC()
	r, b, d := 10, 20, 30
	parent := scenario.Scenario{ID: "scenario-1", TenantID: "tenant-1", TriggerObjectType: "transactions"}
	iteration := scenario.Iteration{ID: "pinned-old", TenantID: "tenant-1", ScenarioID: parent.ID, Status: scenario.IterationStatusCommitted, CommittedAt: &at, TriggerFormula: formula, ScoreReviewThreshold: &r, ScoreBlockAndReviewThreshold: &b, ScoreDeclineThreshold: &d}
	rule := scenario.Rule{ID: "rule-1", TenantID: "tenant-1", IterationID: iteration.ID, Name: "Old score", Formula: []byte(`{"constant":true}`), ScoreModifier: 15}
	pin, err := NewPinnedScenarioSnapshot(parent, iteration, []scenario.Rule{rule}, ExecutionPreparationLimits{10, 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := PrepareObjectBatch(context.Background(), "tenant-1", "transactions", []DecisionEvaluationRequest{{ObjectID: "exact ", ObjectType: "transactions", Fields: map[string]any{"amount": 12}}}, preparationModel(), nil, ExecutionPreparationLimits{10, 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the original threshold/formula cannot redirect pinned evaluation.
	r = 1000
	rule.ScoreModifier = 500
	return pin, batch
}
func TestPinnedPreparationUsesRealEvaluatorWithoutPersistenceOrLiveCache(t *testing.T) {
	pin, batch := preparedPinFixture(t, []byte(`{"constant":true}`))
	svc := newFailureTestDecisionService(nil, nilScoringConfigRepository(nil), nilScoringRequestRepository(nil), nilScreeningConfigRepository(nil))
	// Pinned evaluation must not even call these live repositories, with or
	// without a newer publication, cache hit or missing current live link.
	svc.scenarioRepo = nil
	svc.iterationRepo = nil
	svc.ruleRepo = nil
	svc.dataModelReader = nil
	plans, err := svc.PreparePinnedBatch(context.Background(), pin, batch)
	if err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	decisions, evidence := &baselineDecisionRepo{}, &baselineEvidenceRepo{}
	if decisions.stored.ID != "" {
		t.Fatal("preparation wrote decision")
	}
	store := decisionMutationStoreStub{decisionRepo: decisions, ruleExecutionRepo: evidence, workflowExecutionRepo: workflowExecutionRepoStub{}, screeningExecutionRepo: nilScreeningExecutionRepository(nil), scoringRequestRepo: nilScoringRequestRepository(nil)}
	result, err := plans[0].Persist(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision == nil || result.Decision.ScenarioIterationID != "pinned-old" || result.Decision.ObjectID != "exact " || result.Decision.Score != 15 || result.Decision.Outcome != decision.OutcomeReview || len(result.RuleExecutions) != 1 {
		t.Fatal("not pinned", result)
	}
	if _, err := plans[0].Persist(context.Background(), store); !errors.Is(err, ErrExecutionPreparation) {
		t.Fatal("plan reused", err)
	}
	wrong := batch
	wrong.tenant = "other"
	if _, err := svc.PreparePinnedBatch(context.Background(), pin, wrong); !errors.Is(err, ErrExecutionPreparation) {
		t.Fatal("cross tenant batch", err)
	}
	badPin := pin
	badPin.rules = append([]scenario.Rule(nil), pin.rules...)
	badPin.rules[0].Formula = []byte(`{"function":"eq","children":[{"function":"Payload","children":[{"constant":"missing"}]},{"constant":1}]}`)
	if _, err := svc.PreparePinnedBatch(context.Background(), badPin, batch); err == nil {
		t.Fatal("incompatible old formula accepted")
	}
	payloadPin := pin
	payloadPin.rules = append([]scenario.Rule(nil), pin.rules...)
	payloadPin.rules[0].Formula = []byte(`{"function":"gt","children":[{"function":"Payload","children":[{"constant":"amount"}]},{"constant":10}]}`)
	repeated, err := PrepareObjectBatch(context.Background(), "tenant-1", "transactions", []DecisionEvaluationRequest{{ObjectID: "same", ObjectType: "transactions", Fields: map[string]any{"amount": 1}}, {ObjectID: "same", ObjectType: "transactions", Fields: map[string]any{"amount": 20}}}, preparationModel(), nil, ExecutionPreparationLimits{10, 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	repeatedPlans, err := svc.PreparePinnedBatch(context.Background(), payloadPin, repeated)
	if err != nil {
		t.Fatal(err)
	}
	for i, plan := range repeatedPlans {
		result, err := plan.Persist(context.Background(), store)
		if err != nil || result.Decision.Score != i*15 {
			t.Fatal("cross-item cache contamination", i, result, err)
		}
	}
	noHit, batch := preparedPinFixture(t, []byte(`{"constant":false}`))
	plans, err = svc.PreparePinnedBatch(context.Background(), noHit, batch)
	if err != nil {
		t.Fatal(err)
	}
	result, err = plans[0].Persist(context.Background(), store)
	if err != nil || result.Triggered || result.Decision != nil {
		t.Fatal("non-triggered batch lost", result, err)
	}
}
