package ast_eval

import (
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type Runtime struct {
	TenantID                    string
	ObjectID                    string
	ObjectType                  string
	Fields                      map[string]any
	Now                         time.Time
	Model                       *ports.TenantModel
	TenantDataReader            ports.TenantDataReader
	CustomListRepo              ports.CustomListRepository
	RecordTagRepo               ports.RecordTagRepository
	RiskRepo                    ports.RiskSnapshotRepository
	IPFlagRepo                  ports.IPFlagRepository
	DecisionRepo                ports.DecisionRepository
	AggregatePushdownMode       string
	AggregatePushdownAggregates []string
}

const (
	AggregatePushdownModeEnabled  = "enabled"
	AggregatePushdownModeDisabled = "disabled"
	AggregatePushdownModeStrict   = "strict"
)

func (r Runtime) aggregatePushdownMode() string {
	switch r.AggregatePushdownMode {
	case AggregatePushdownModeDisabled:
		return AggregatePushdownModeDisabled
	case AggregatePushdownModeStrict:
		return AggregatePushdownModeStrict
	default:
		return AggregatePushdownModeEnabled
	}
}

func (r Runtime) aggregatePushdownEnabled() bool {
	return r.aggregatePushdownMode() != AggregatePushdownModeDisabled
}

func (r Runtime) aggregatePushdownStrict() bool {
	return r.aggregatePushdownMode() == AggregatePushdownModeStrict
}

func (r Runtime) aggregatePushdownSupportsAggregate(name string) bool {
	if len(r.AggregatePushdownAggregates) == 0 {
		return true
	}
	canonical := strings.ToLower(strings.TrimSpace(name))
	for _, item := range r.AggregatePushdownAggregates {
		if strings.ToLower(strings.TrimSpace(item)) == canonical {
			return true
		}
	}
	return false
}

type ScoreComputationResult struct {
	Triggered bool `json:"triggered"`
	Modifier  int  `json:"modifier"`
	Floor     int  `json:"floor"`

	Branch   *int `json:"branch,omitempty"`
	Fallback bool `json:"fallback"`
	Default  bool `json:"default"`
}
