package mysqlexecution

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"time"
)

type EvaluationPolicy struct {
	InlineScreeningEvaluator                                   screening.InlineEvaluator
	Limits                                                     service.ExecutionPreparationLimits
	PublishOutboxEvents                                        bool
	DeferAdditionalScoring                                     bool
	DeferExternalEffects                                       bool
	Aggregates                                                 []string
	RuleConcurrency, ScenarioConcurrency, AggregateConcurrency int
	AggregateTimeout, AggregateBatchWindow                     time.Duration
	AggregateExecutor                                          *asteval.AggregateExecutor
	FormulaPlans                                               *asteval.FormulaPlans
}

// NewTargetPreparer connects actual same-scope SQL read adapters to the shared
// evaluator. Live-cache/scenario repos and the legacy transaction manager are
// deliberately absent: this component only exposes validated prepared execution,
// with output persistence delegated to the fenced target unit of work.
// GeoIP is a separate explicit optional provider; its function fails if absent.
func NewTargetPreparer(owner *mysql.ExecutionStore, reader mysql.TenantDataReader, reads mysql.EvaluationReaders, ids ports.IDGenerator, clock ports.Clock, geo ports.GeoIPLookup, policy EvaluationPolicy) (*Preparer, error) {
	if !reads.MatchesExecutionStore(owner) || ids == nil || clock == nil || len(policy.Aggregates) == 0 || len(policy.Aggregates) > 6 {
		return nil, service.ErrExecutionPreparation
	}
	seen := map[string]bool{}
	for _, name := range policy.Aggregates {
		switch name {
		case "count", "count_distinct", "sum", "avg", "min", "max":
		default:
			return nil, service.ErrExecutionPreparation
		}
		if seen[name] {
			return nil, service.ErrExecutionPreparation
		}
		seen[name] = true
	}
	for _, limit := range []int{policy.RuleConcurrency, policy.ScenarioConcurrency, policy.AggregateConcurrency} {
		if limit < 0 || limit > 1024 {
			return nil, service.ErrExecutionPreparationLimit
		}
	}
	if policy.AggregateTimeout != 0 && policy.AggregateExecutor == nil {
		executor, err := asteval.NewAggregateExecutor(policy.AggregateConcurrency, policy.AggregateTimeout, policy.AggregateBatchWindow)
		if err != nil {
			return nil, err
		}
		policy.AggregateExecutor = executor
	} else if policy.AggregateBatchWindow != 0 && policy.AggregateTimeout == 0 {
		return nil, service.ErrExecutionPreparation
	}
	if policy.FormulaPlans == nil {
		plans, err := asteval.NewFormulaPlans(256, 8<<20)
		if err != nil {
			return nil, err
		}
		policy.FormulaPlans = plans
	}
	scoringConfigs := reads.ScoringConfigs()
	if policy.DeferAdditionalScoring {
		scoringConfigs = deferredScoringConfigs{scoringConfigs}
	}
	workflows, screeningConfigs := reads.Workflows(), reads.ScreeningConfigs()
	workflowRules := reads.WorkflowRules()
	if policy.DeferExternalEffects {
		if !policy.DeferAdditionalScoring || policy.PublishOutboxEvents {
			return nil, mysql.ErrOwnerContractUnavailable
		}
		workflows = deferredWorkflows{workflows}
		workflowRules = deferredWorkflowRules{workflowRules}
		screeningConfigs = deferredScreening{screeningConfigs}
	}
	evaluator := service.NewDecisionService(nil, ids, clock, nil, nil, nil, nil, reader, nil, nil, workflows, workflowRules, nil, nil, nil, reads.Snoozes(), nil, reads.CustomLists(), reads.RecordTags(), reads.RiskSnapshots(), reads.IPFlags(), screeningConfigs, nil, scoringConfigs, nil, policy.PublishOutboxEvents, "enabled", policy.Aggregates, policy.RuleConcurrency, policy.ScenarioConcurrency, policy.AggregateConcurrency, nil)
	evaluator.SetGeoIPLookup(geo)
	evaluator.InlineScreeningEvaluator = policy.InlineScreeningEvaluator
	evaluator = evaluator.WithEvaluationOptimizations(policy.AggregateExecutor, policy.FormulaPlans)
	if !policy.DeferAdditionalScoring && !policy.DeferExternalEffects {
		evaluator = evaluator.WithEvaluationRoutingReader(reads)
	}
	return NewPreparer(owner, reader, evaluator, policy.Limits)
}

// Recheck on every preparation so a late configuration change fails explicitly
// rather than silently omitting an effect selected by the scenario.
type deferredScoringConfigs struct{ ports.EvaluationScoringConfigs }

func (r deferredScoringConfigs) ListActiveByScenario(ctx context.Context, tenant, scenario string) ([]scoring.Config, error) {
	items, err := r.EvaluationScoringConfigs.ListActiveByScenario(ctx, tenant, scenario)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return nil, mysql.ErrAdditionalScoringDeferred
	}
	return items, nil
}
