package mysqlexecution

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
)

type EvaluationPolicy struct {
	Limits                                                     service.ExecutionPreparationLimits
	PublishOutboxEvents                                        bool
	Aggregates                                                 []string
	RuleConcurrency, ScenarioConcurrency, AggregateConcurrency int
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
	evaluator := service.NewDecisionService(nil, ids, clock, nil, nil, nil, nil, reader, nil, nil, reads.Workflows(), reads.WorkflowRules(), nil, nil, nil, reads.Snoozes(), nil, reads.CustomLists(), reads.RecordTags(), reads.RiskSnapshots(), reads.IPFlags(), reads.ScreeningConfigs(), nil, reads.ScoringConfigs(), nil, policy.PublishOutboxEvents, "enabled", policy.Aggregates, policy.RuleConcurrency, policy.ScenarioConcurrency, policy.AggregateConcurrency, nil)
	evaluator.SetGeoIPLookup(geo)
	return NewPreparer(owner, reader, evaluator, policy.Limits)
}
