package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type multiScenarioRepoStub struct {
	scenarios []scenario.Scenario
}

func (s multiScenarioRepoStub) Create(context.Context, scenario.Scenario) (scenario.Scenario, error) {
	return scenario.Scenario{}, nil
}
func (s multiScenarioRepoStub) GetByID(_ context.Context, _ string, id string) (scenario.Scenario, error) {
	for _, item := range s.scenarios {
		if item.ID == id {
			return item, nil
		}
	}
	return scenario.Scenario{}, errors.New("not found")
}
func (s multiScenarioRepoStub) ListByTenant(context.Context, string) ([]scenario.Scenario, error) {
	return s.scenarios, nil
}
func (s multiScenarioRepoStub) ListLiveByTriggerObject(_ context.Context, _ string, objectType string) ([]scenario.Scenario, error) {
	var out []scenario.Scenario
	for _, item := range s.scenarios {
		if item.TriggerObjectType == objectType && item.LiveIterationID != nil {
			out = append(out, item)
		}
	}
	return out, nil
}
func (s multiScenarioRepoStub) Update(context.Context, scenario.Scenario) (scenario.Scenario, error) {
	return scenario.Scenario{}, nil
}
func (s multiScenarioRepoStub) SetLiveIterationID(context.Context, string, string, *string) error {
	return nil
}
func (s multiScenarioRepoStub) Delete(context.Context, string, string) error {
	return nil
}

type multiIterationRepoStub struct {
	iterations map[string]scenario.Iteration
}

func (s multiIterationRepoStub) Create(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (s multiIterationRepoStub) GetByID(_ context.Context, _, _, id string) (scenario.Iteration, error) {
	if item, ok := s.iterations[id]; ok {
		return item, nil
	}
	return scenario.Iteration{}, errors.New("not found")
}
func (s multiIterationRepoStub) ListByScenario(context.Context, string, string) ([]scenario.Iteration, error) {
	return nil, nil
}
func (s multiIterationRepoStub) Commit(context.Context, string, string, string, time.Time) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}
func (s multiIterationRepoStub) ListLiveScheduled(context.Context, int) ([]scenario.Iteration, error) {
	return nil, nil
}
func (s multiIterationRepoStub) NextVersion(context.Context, string, string) (int, error) {
	return 1, nil
}
func (s multiIterationRepoStub) Update(context.Context, scenario.Iteration) (scenario.Iteration, error) {
	return scenario.Iteration{}, nil
}

type overloadedOnSecondAggregateReader struct {
	calls atomic.Int32
}

func (r *overloadedOnSecondAggregateReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}
func (r *overloadedOnSecondAggregateReader) ListRecords(context.Context, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (r *overloadedOnSecondAggregateReader) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (r *overloadedOnSecondAggregateReader) AggregateRecords(_ context.Context, _ string, query ports.AggregateQuery) (any, error) {
	if query.Field == "fail_overload" {
		return nil, asteval.ErrAggregateOverloaded
	}
	r.calls.Add(1)
	return int64(5), nil
}

type countingTxManager struct {
	runs             atomic.Int32
	decisionsCreated atomic.Int32
	outboxCreated    atomic.Int32
}

func (m *countingTxManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	m.runs.Add(1)
	store := decisionMutationStoreStub{
		decisionRepo:           countingDecisionRepo{counter: &m.decisionsCreated},
		ruleExecutionRepo:      nilRuleExecutionRepository(nil),
		workflowExecutionRepo:  workflowExecutionRepoStub{},
		screeningExecutionRepo: nilScreeningExecutionRepository(nil),
		scoringRequestRepo:     nilScoringRequestRepository(nil),
		outboxRepo:             countingOutboxRepo{counter: &m.outboxCreated},
	}
	return fn(store)
}

type countingDecisionRepo struct {
	decisionCreateRepoStub
	counter *atomic.Int32
}

func (r countingDecisionRepo) Create(ctx context.Context, item decision.Decision) (decision.Decision, error) {
	r.counter.Add(1)
	return r.decisionCreateRepoStub.Create(ctx, item)
}

type countingOutboxRepo struct {
	nilOutboxEventRepository
	counter *atomic.Int32
}

func (r countingOutboxRepo) CreateMany(ctx context.Context, items []integration.OutboxEvent) ([]integration.OutboxEvent, error) {
	r.counter.Add(int32(len(items)))
	return items, nil
}

func TestEvaluateAllLiveScenariosCommitsZeroDecisionsWhenOneScenarioIsOverloaded(t *testing.T) {
	t.Parallel()

	trueFormula, _ := json.Marshal(domainast.Node{Constant: true})
	overloadRuleFormula, _ := json.Marshal(domainast.Node{
		Function: ">",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "fail_overload"},
					"aggregator": {Constant: "COUNT"},
				},
			},
			{Constant: 1},
		},
	})

	iter1 := "iter-1"
	iter2 := "iter-2"
	review := 10
	block := 50
	decline := 100

	tx := &countingTxManager{}
	svc := NewDecisionService(
		tx,
		fixedIDGenerator{id: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		fixedClock{now: time.Date(2026, 11, 25, 20, 0, 0, 0, time.UTC)},
		&publicationDataModelReader{
			model: ports.TenantModel{
				RevisionID:        "rev-1",
				RecordLookupField: "object_id",
				Tables: map[string]ports.TenantModelTable{
					"transactions": {
						ID:   "t1",
						Name: "transactions",
						Fields: map[string]ports.TenantModelField{
							"object_id":     {Name: "object_id", Type: "string"},
							"fail_overload": {Name: "fail_overload", Type: "string"},
						},
					},
				},
			},
		},
		multiScenarioRepoStub{scenarios: []scenario.Scenario{
			{ID: "s1", TenantID: "tenant-1", TriggerObjectType: "transactions", LiveIterationID: &iter1},
			{ID: "s2", TenantID: "tenant-1", TriggerObjectType: "transactions", LiveIterationID: &iter2},
		}},
		multiIterationRepoStub{iterations: map[string]scenario.Iteration{
			iter1: {ID: iter1, ScenarioID: "s1", TenantID: "tenant-1", TriggerFormula: trueFormula, ScoreReviewThreshold: &review, ScoreBlockAndReviewThreshold: &block, ScoreDeclineThreshold: &decline},
			iter2: {ID: iter2, ScenarioID: "s2", TenantID: "tenant-1", TriggerFormula: trueFormula, ScoreReviewThreshold: &review, ScoreBlockAndReviewThreshold: &block, ScoreDeclineThreshold: &decline},
		}},
		ruleListMapStub{byIter: map[string][]scenario.Rule{
			iter1: {{ID: "r1", TenantID: "tenant-1", IterationID: iter1, Name: "fast-hit", Formula: trueFormula, ScoreModifier: 60}},
			iter2: {{ID: "r2", TenantID: "tenant-1", IterationID: iter2, Name: "overloaded-agg", Formula: overloadRuleFormula, ScoreModifier: 60}},
		}},
		&overloadedOnSecondAggregateReader{},
		countingDecisionRepo{counter: &tx.decisionsCreated},
		nilRuleExecutionRepository(nil),
		&workflowRepoStub{},
		noopWorkflowRuleRepo{},
		noopWorkflowConditionRepo{},
		noopWorkflowActionRepo{},
		workflowExecutionRepoStub{},
		nilRuleSnoozeRepository(nil),
		countingOutboxRepo{counter: &tx.outboxCreated},
		stubCustomListRepo{},
		stubRecordTagRepo{},
		stubRiskRepo{},
		stubIPFlagRepo{},
		nilScreeningConfigRepository(nil),
		nilScreeningExecutionRepository(nil),
		nilScoringConfigRepository(nil),
		nilScoringRequestRepository(nil),
		nil,
		nil,
		nil,
		nil,
		asteval.AggregatePushdownModeEnabled,
		[]string{"count"},
		1,
		1, // sequential scenario execution: s1 finishes first, then s2 hits ErrAggregateOverloaded
		0,
		nil,
	)
	svc.InlinePolicy.Enabled = false // specifically verify staging even when InlinePolicy is disabled

	_, err := svc.EvaluateAllLiveScenarios(context.Background(), "tenant-1", DecisionEvaluationRequest{
		ObjectID:   "txn-1",
		ObjectType: "transactions",
		Fields:     map[string]any{"object_id": "txn-1", "fail_overload": "x"},
	})
	if !errors.Is(err, asteval.ErrAggregateOverloaded) {
		t.Fatalf("EvaluateAllLiveScenarios error = %v, want ErrAggregateOverloaded", err)
	}
	if tx.runs.Load() != 0 || tx.decisionsCreated.Load() != 0 || tx.outboxCreated.Load() != 0 {
		t.Fatalf("expected zero committed rows before deferral, got txRuns=%d decisions=%d outbox=%d",
			tx.runs.Load(), tx.decisionsCreated.Load(), tx.outboxCreated.Load())
	}
}

type ruleListMapStub struct {
	byIter map[string][]scenario.Rule
}

func (s ruleListMapStub) Create(context.Context, scenario.Rule) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (s ruleListMapStub) GetByID(context.Context, string, string, string, string) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (s ruleListMapStub) ListByIteration(_ context.Context, _, _, iterID string) ([]scenario.Rule, error) {
	return s.byIter[iterID], nil
}
func (s ruleListMapStub) ListRuleGroupsByScenario(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (s ruleListMapStub) Update(context.Context, scenario.Rule) (scenario.Rule, error) {
	return scenario.Rule{}, nil
}
func (s ruleListMapStub) Delete(context.Context, string, string, string, string) error {
	return nil
}
