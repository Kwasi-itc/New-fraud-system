package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

func TestEvaluateRulesPreservesOrderAndSnoozes(t *testing.T) {
	t.Parallel()

	snoozeGroupID := "sg-1"
	rules := []scenario.Rule{
		{
			ID:            "rule-1",
			Name:          "matched",
			ScoreModifier: 30,
			Formula:       []byte(`{"function":"eq","children":[{"constant":"review"},{"constant":"review"}]}`),
		},
		{
			ID:            "rule-2",
			Name:          "snoozed",
			ScoreModifier: 50,
			SnoozeGroupID: &snoozeGroupID,
			Formula:       []byte(`{"constant":true}`),
		},
		{
			ID:            "rule-3",
			Name:          "not-matched",
			ScoreModifier: 70,
			Formula:       []byte(`{"constant":false}`),
		},
	}

	results, err := evaluateRules(
		context.Background(),
		rules,
		asteval.Runtime{TenantID: "tenant-1", ObjectID: "obj-1", ObjectType: "transactions", Fields: map[string]any{}},
		map[string]struct{}{snoozeGroupID: {}},
		2,
	)
	if err != nil {
		t.Fatalf("evaluateRules() error = %v", err)
	}
	if len(results) != len(rules) {
		t.Fatalf("evaluateRules() len = %d, want %d", len(results), len(rules))
	}

	if results[0].Rule.ID != "rule-1" || !results[0].Matched || results[0].Snoozed {
		t.Fatalf("unexpected result[0] = %#v", results[0])
	}
	if results[1].Rule.ID != "rule-2" || results[1].Matched || !results[1].Snoozed {
		t.Fatalf("unexpected result[1] = %#v", results[1])
	}
	if results[2].Rule.ID != "rule-3" || results[2].Matched || results[2].Snoozed {
		t.Fatalf("unexpected result[2] = %#v", results[2])
	}
}

func TestEvaluateScenarioByIterationSupportsAdvancedAggregationRules(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	result, err := evaluateScenarioByIteration(
		context.Background(),
		ruleTestIDGen{value: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		ruleTestClock{now: now},
		"tenant-1",
		"scenario-1",
		"iter-1",
		DecisionEvaluationRequest{
			ObjectID:   "txn-1",
			ObjectType: "transactions",
			Fields: map[string]any{
				"object_id":  "txn-1",
				"owner_id":   "customer-1",
				"created_at": now.Format(time.RFC3339),
			},
		},
		scenarioIterationRepoStub{
			iteration: scenario.Iteration{
				ID:                           "iter-1",
				ScenarioID:                   "scenario-1",
				TenantID:                     "tenant-1",
				Status:                       scenario.IterationStatusCommitted,
				TriggerFormula:               []byte(`{"constant":true}`),
				ScoreReviewThreshold:         intPtrTest(10),
				ScoreBlockAndReviewThreshold: intPtrTest(20),
				ScoreDeclineThreshold:        intPtrTest(30),
			},
		},
		ruleRepoStub{
			rules: []scenario.Rule{
				{
					ID:            "rule-agg",
					Name:          "recent-distinct-activity",
					ScoreModifier: 15,
					Formula: []byte(`{
						"function":"eq",
						"children":[
							{
								"function":"Aggregator",
								"named_children":{
									"tableName":{"constant":"transactions"},
									"fieldName":{"constant":"object_id"},
									"aggregator":{"constant":"COUNT_DISTINCT"},
									"filters":{
										"function":"List",
										"children":[
											{
												"function":"Filter",
												"named_children":{
													"tableName":{"constant":"transactions"},
													"fieldName":{"constant":"owner_id"},
													"operator":{"constant":"="},
													"value":{"function":"Payload","children":[{"constant":"owner_id"}]}
												}
											},
											{
												"function":"Filter",
												"named_children":{
													"tableName":{"constant":"transactions"},
													"fieldName":{"constant":"created_at"},
													"operator":{"constant":">="},
													"value":{
														"function":"TimeAdd",
														"named_children":{
															"timestampField":{"function":"Payload","children":[{"constant":"created_at"}]},
															"duration":{"constant":"PT24H"},
															"sign":{"constant":"-"}
														}
													}
												}
											}
										]
									}
								}
							},
							{"constant":2}
						]
					}`),
				},
			},
		},
		dataModelReaderStub{
			model: ports.TenantModel{
				RevisionID:        "rev-1",
				RecordLookupField: "object_id",
				Tables: map[string]ports.TenantModelTable{
					"transactions": {
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"object_id":  {Name: "object_id", Type: "string"},
							"owner_id":   {Name: "owner_id", Type: "string"},
							"created_at": {Name: "created_at", Type: "timestamp"},
						},
					},
				},
			},
		},
		stubTenantDataReader{
			records: []ports.TenantRecord{
				{ObjectID: "txn-1", ObjectType: "transactions", Fields: map[string]any{"object_id": "txn-1", "owner_id": "customer-1", "created_at": now.Add(-1 * time.Hour).Format(time.RFC3339)}},
				{ObjectID: "txn-2", ObjectType: "transactions", Fields: map[string]any{"object_id": "txn-2", "owner_id": "customer-1", "created_at": now.Add(-2 * time.Hour).Format(time.RFC3339)}},
				{ObjectID: "txn-3", ObjectType: "transactions", Fields: map[string]any{"object_id": "txn-3", "owner_id": "customer-2", "created_at": now.Add(-3 * time.Hour).Format(time.RFC3339)}},
			},
		},
		stubDecisionRepo{},
		nil,
		nil,
		nil,
		nil,
		asteval.AggregatePushdownModeEnabled,
		nil,
	)
	if err != nil {
		t.Fatalf("evaluateScenarioByIteration() error = %v", err)
	}
	if !result.Triggered {
		t.Fatalf("evaluateScenarioByIteration() Triggered = false, want true")
	}
	if result.Decision == nil {
		t.Fatalf("evaluateScenarioByIteration() Decision = nil")
	}
	if result.Decision.Score != 15 {
		t.Fatalf("evaluateScenarioByIteration() score = %d, want 15", result.Decision.Score)
	}
	if result.Decision.Outcome != "review" {
		t.Fatalf("evaluateScenarioByIteration() outcome = %s, want review", result.Decision.Outcome)
	}
	if len(result.RuleExecutions) != 1 || result.RuleExecutions[0].Outcome != "hit" {
		t.Fatalf("evaluateScenarioByIteration() rule executions = %#v", result.RuleExecutions)
	}
}

func TestEvaluateScenarioByIterationTreatsMissingFieldComparisonAsNoHit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	result, err := evaluateScenarioByIteration(
		context.Background(),
		ruleTestIDGen{value: uuid.MustParse("22222222-2222-2222-2222-222222222222")},
		ruleTestClock{now: now},
		"tenant-1",
		"scenario-1",
		"iter-1",
		DecisionEvaluationRequest{
			ObjectID:   "txn-1",
			ObjectType: "transactions",
			Fields: map[string]any{
				"object_id": "txn-1",
			},
		},
		scenarioIterationRepoStub{
			iteration: scenario.Iteration{
				ID:                           "iter-1",
				ScenarioID:                   "scenario-1",
				TenantID:                     "tenant-1",
				Status:                       scenario.IterationStatusCommitted,
				TriggerFormula:               []byte(`{"constant":true}`),
				ScoreReviewThreshold:         intPtrTest(10),
				ScoreBlockAndReviewThreshold: intPtrTest(20),
				ScoreDeclineThreshold:        intPtrTest(30),
			},
		},
		ruleRepoStub{
			rules: []scenario.Rule{
				{
					ID:            "rule-amount",
					Name:          "Test transaction amount",
					ScoreModifier: 100,
					Formula: []byte(`{
						"function":"gt",
						"children":[
							{"function":"field_ref","named_children":{"field":{"constant":"amount"}}},
							{"constant":1000}
						]
					}`),
				},
			},
		},
		dataModelReaderStub{
			model: ports.TenantModel{
				RevisionID:        "rev-1",
				RecordLookupField: "object_id",
				Tables: map[string]ports.TenantModelTable{
					"transactions": {
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"object_id": {Name: "object_id", Type: "string"},
							"amount":    {Name: "amount", Type: "number"},
						},
					},
				},
			},
		},
		stubTenantDataReader{},
		stubDecisionRepo{},
		nil,
		nil,
		nil,
		nil,
		asteval.AggregatePushdownModeEnabled,
		nil,
	)
	if err != nil {
		t.Fatalf("evaluateScenarioByIteration() error = %v", err)
	}
	if !result.Triggered {
		t.Fatalf("evaluateScenarioByIteration() Triggered = false, want true")
	}
	if result.Decision == nil {
		t.Fatalf("evaluateScenarioByIteration() Decision = nil")
	}
	if result.Decision.Score != 0 {
		t.Fatalf("evaluateScenarioByIteration() score = %d, want 0", result.Decision.Score)
	}
	if len(result.RuleExecutions) != 1 || result.RuleExecutions[0].Outcome != "no_hit" {
		t.Fatalf("evaluateScenarioByIteration() rule executions = %#v", result.RuleExecutions)
	}
}

func TestResolveRuleEvaluationConcurrency(t *testing.T) {
	t.Parallel()

	if got := resolveRuleEvaluationConcurrency(0, 3); got < 1 || got > 3 {
		t.Fatalf("resolveRuleEvaluationConcurrency(default, 3) = %d, want within [1,3]", got)
	}
	if got := resolveRuleEvaluationConcurrency(10, 3); got != 3 {
		t.Fatalf("resolveRuleEvaluationConcurrency(10, 3) = %d, want 3", got)
	}
	if got := resolveRuleEvaluationConcurrency(2, 3); got != 2 {
		t.Fatalf("resolveRuleEvaluationConcurrency(2, 3) = %d, want 2", got)
	}
}

type ruleTestClock struct {
	now time.Time
}

func (f ruleTestClock) Now() time.Time {
	return f.now
}

type ruleTestIDGen struct {
	value uuid.UUID
}

func (f ruleTestIDGen) New() uuid.UUID {
	return f.value
}

type scenarioIterationRepoStub struct {
	iteration scenario.Iteration
}

func (s scenarioIterationRepoStub) Create(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (s scenarioIterationRepoStub) ListByScenario(context.Context, string, string) ([]scenario.Iteration, error) {
	return nil, nil
}
func (s scenarioIterationRepoStub) ListLiveScheduled(context.Context, int) ([]scenario.Iteration, error) {
	return nil, nil
}
func (s scenarioIterationRepoStub) NextVersion(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s scenarioIterationRepoStub) GetByID(context.Context, string, string, string) (scenario.Iteration, error) {
	return s.iteration, nil
}
func (s scenarioIterationRepoStub) Commit(context.Context, string, string, string, time.Time) (scenario.Iteration, error) {
	return s.iteration, nil
}
func (s scenarioIterationRepoStub) Update(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return s.iteration, nil
}

type ruleRepoStub struct {
	rules []scenario.Rule
}

type dataModelReaderStub struct {
	model ports.TenantModel
}

func (s dataModelReaderStub) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return s.model, nil
}
func (s dataModelReaderStub) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, nil
}
func (s dataModelReaderStub) RetryIndexJob(context.Context, string) error {
	return nil
}

func (s ruleRepoStub) Create(context.Context, scenario.Rule) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (s ruleRepoStub) ListByIteration(context.Context, string, string, string) ([]scenario.Rule, error) {
	return s.rules, nil
}
func (s ruleRepoStub) GetByID(context.Context, string, string, string, string) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (s ruleRepoStub) Update(context.Context, scenario.Rule) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (s ruleRepoStub) Delete(context.Context, string, string, string, string) error {
	return nil
}

func intPtrTest(v int) *int {
	return &v
}

var (
	_ ports.Clock                       = ruleTestClock{}
	_ ports.IDGenerator                 = ruleTestIDGen{}
	_ ports.ScenarioIterationRepository = scenarioIterationRepoStub{}
	_ ports.RuleRepository              = ruleRepoStub{}
	_ ports.DataModelReader             = dataModelReaderStub{}
)
