package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"math"
)

// PrepareComparison evaluates both versions with one normalized object/model.
// It creates no workflows, external requests or live decisions. The owner
// persists only phantom evidence after rechecking the test run and draft.
func (s DecisionService) PrepareComparison(ctx context.Context, live, phantom PinnedScenarioSnapshot, batch PreparedObjectBatch) (TestRunEvaluationResult, error) {
	if batch.Len() != 1 || s.idGen == nil || s.clock == nil {
		return TestRunEvaluationResult{}, ErrExecutionPreparation
	}
	evaluate := func(pin PinnedScenarioSnapshot) (DecisionEvaluationResult, error) {
		if pin.TenantID() != batch.tenant || pin.ObjectType() != batch.objectType {
			return DecisionEvaluationResult{}, ErrExecutionPreparation
		}
		scope, err := pin.modelPlanScope(batch.model)
		if err != nil {
			return DecisionEvaluationResult{}, err
		}
		if err := s.formulaPlans.ValidateScope(batch.tenant, scope, func() error { return ValidatePinnedModel(pin, batch.model) }); err != nil {
			return DecisionEvaluationResult{}, err
		}
		item := batch.items[0]
		if item.prepared == nil {
			return DecisionEvaluationResult{}, ErrExecutionPreparation
		}
		model := item.prepared.model
		runtime := asteval.Runtime{TenantID: batch.tenant, ObjectID: item.ObjectID, ObjectType: item.ObjectType, Fields: item.Fields, Now: s.clock.Now(), Model: &model, TenantDataReader: item.prepared.reader, DecisionRepo: s.decisionRepo, CustomListRepo: s.customListRepo, RecordTagRepo: s.recordTagRepo, RiskRepo: s.riskRepo, IPFlagRepo: s.ipFlagRepo, GeoIPLookup: s.geoIPLookup, AggregatePushdownMode: s.aggregatePushdownMode, AggregatePushdownAggregates: s.aggregatePushdownAggregates, AggregateResultCache: asteval.NewAggregateResultCache(), GeoIPResultCache: asteval.NewGeoIPResultCache()}
		runtime.AggregateBatcher = asteval.NewAggregateBatcher(ctx, batch.tenant, item.prepared.reader, s.aggregateExecutor)
		runtime.FormulaPlans, runtime.PlanScope = s.formulaPlans, scope
		triggered, err := asteval.EvaluateFormula(ctx, pin.iteration.TriggerFormula, runtime)
		if err != nil {
			return DecisionEvaluationResult{}, err
		}
		if !triggered {
			return DecisionEvaluationResult{ModelRevision: model.RevisionID, Triggered: false}, nil
		}
		rules, err := evaluateRules(ctx, pin.rules, runtime, nil, s.ruleEvaluationConcurrency)
		if err != nil {
			return DecisionEvaluationResult{}, err
		}
		now := s.clock.Now()
		id := s.idGen.New().String()
		score := int64(0)
		executions := make([]decision.RuleExecution, len(rules))
		for i, rule := range rules {
			if rule.Matched {
				score += int64(rule.Rule.ScoreModifier)
			}
			executions[i] = newRuleExecution(now, id, rule.Rule, rule.Matched, rule.Evaluation)
			executions[i].ID = s.idGen.New().String()
		}
		if score < math.MinInt32 || score > math.MaxInt32 {
			return DecisionEvaluationResult{}, ErrExecutionPreparationLimit
		}
		value := decision.Decision{ID: id, TenantID: pin.TenantID(), ScenarioID: pin.ScenarioID(), ScenarioIterationID: pin.IterationID(), ObjectID: item.ObjectID, ObjectType: item.ObjectType, Outcome: outcomeFromScore(int(score), pin.iteration), Score: int(score), Triggered: true, CreatedAt: now}
		return DecisionEvaluationResult{ModelRevision: model.RevisionID, Triggered: true, Decision: &value, RuleExecutions: executions}, nil
	}
	left, err := evaluate(live)
	if err != nil {
		return TestRunEvaluationResult{}, err
	}
	right, err := evaluate(phantom)
	if err != nil {
		return TestRunEvaluationResult{}, err
	}
	return TestRunEvaluationResult{Live: left, Phantom: right}, nil
}
