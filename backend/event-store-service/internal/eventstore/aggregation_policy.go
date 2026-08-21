package eventstore

import (
	"errors"
	"fmt"
	"strings"
)

const (
	AggregationModeProjectionOnly = "projection_only"
	AggregationModeAdaptiveCache  = "adaptive_cache"
	AggregationModeTieredSummary  = "tiered_summary"
	AggregationModeAlwaysOnline   = "always_online"

	AggregationColdQueryClickHouse = "query_clickhouse"
	AggregationColdDurableSummary  = "durable_summary"
	AggregationColdDeferAsync      = "defer_async"
	AggregationColdSkipRule        = "skip_rule"
	AggregationColdUseDefault      = "use_default"
)

// ErrAggregationDeferred tells a synchronous decision caller that the selected
// cold policy requires async evaluation while the durable summary is built.
var ErrAggregationDeferred = errors.New("event aggregation deferred until its durable summary is ready")

// ErrAggregationSkipped is consumed at the decision formula boundary and
// turns that formula into a non-match. Keeping it distinct from nil makes
// skip_rule safe when an aggregate participates in arithmetic or functions.
var ErrAggregationSkipped = errors.New("event aggregation skipped by its cold policy")

type aggregateRuntimePolicy struct {
	Mode         string
	ColdBehavior string
	DefaultValue *float64
	Field        string
}

func normalizeAggregationMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return AggregationModeProjectionOnly
	}
	return value
}

func normalizeAggregationColdBehavior(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return AggregationColdQueryClickHouse
	}
	return value
}

func validateFieldAggregationPolicy(name string, field FieldContract) error {
	mode := normalizeAggregationMode(field.AggregationMode)
	switch mode {
	case AggregationModeProjectionOnly, AggregationModeAdaptiveCache, AggregationModeTieredSummary, AggregationModeAlwaysOnline:
	default:
		return fmt.Errorf("field %s has unsupported aggregation_mode %q", name, field.AggregationMode)
	}
	behavior := normalizeAggregationColdBehavior(field.AggregationColdBehavior)
	switch behavior {
	case AggregationColdQueryClickHouse, AggregationColdDurableSummary, AggregationColdDeferAsync, AggregationColdSkipRule, AggregationColdUseDefault:
	default:
		return fmt.Errorf("field %s has unsupported aggregation_cold_behavior %q", name, field.AggregationColdBehavior)
	}
	if mode == AggregationModeProjectionOnly && behavior != AggregationColdQueryClickHouse {
		return fmt.Errorf("field %s projection_only mode requires query_clickhouse cold behavior", name)
	}
	if mode != AggregationModeProjectionOnly && !field.IsProjection {
		return fmt.Errorf("field %s accelerated aggregation mode requires a ClickHouse projection", name)
	}
	if behavior == AggregationColdUseDefault && field.AggregationDefaultValue == nil {
		return fmt.Errorf("field %s use_default cold behavior requires aggregation_default_value", name)
	}
	if behavior != AggregationColdUseDefault && field.AggregationDefaultValue != nil {
		return fmt.Errorf("field %s aggregation_default_value is only valid with use_default", name)
	}
	return nil
}

func aggregatePolicyForPlan(plan aggregateFactPlan) aggregateRuntimePolicy {
	policy := aggregateRuntimePolicy{Mode: AggregationModeProjectionOnly, ColdBehavior: AggregationColdQueryClickHouse}
	selected := false
	for _, dimension := range plan.Dimensions {
		field := plan.Request.Table.Fields[dimension.Field]
		candidate := policyFromField(dimension.Field, field)
		// A multi-field aggregate is only as explicitly opted-in as its least
		// accelerated dimension. This prevents a tiered merchant policy from
		// accidentally materializing every merchant/account combination when
		// account_ref was deliberately left projection-only.
		if candidate.Mode == AggregationModeProjectionOnly {
			return candidate
		}
		if !selected || aggregationModePriority(candidate.Mode) < aggregationModePriority(policy.Mode) ||
			(aggregationModePriority(candidate.Mode) == aggregationModePriority(policy.Mode) &&
				aggregationColdSafetyPriority(candidate.ColdBehavior) > aggregationColdSafetyPriority(policy.ColdBehavior)) {
			policy = candidate
			selected = true
		}
	}
	// Time-only aggregates have no equality dimension. In that case the user
	// may opt in on the measure field itself.
	if len(plan.Dimensions) == 0 {
		policy = policyFromField(plan.Request.Field, plan.Request.Table.Fields[plan.Request.Field])
	}
	return policy
}

func aggregationColdSafetyPriority(behavior string) int {
	switch behavior {
	case AggregationColdQueryClickHouse:
		return 5
	case AggregationColdDurableSummary:
		return 4
	case AggregationColdDeferAsync:
		return 3
	case AggregationColdSkipRule:
		return 2
	case AggregationColdUseDefault:
		return 1
	default:
		return 0
	}
}

func policyFromField(name string, field FieldContract) aggregateRuntimePolicy {
	return aggregateRuntimePolicy{
		Mode:         normalizeAggregationMode(field.AggregationMode),
		ColdBehavior: normalizeAggregationColdBehavior(field.AggregationColdBehavior),
		DefaultValue: field.AggregationDefaultValue,
		Field:        name,
	}
}

func aggregationModePriority(mode string) int {
	switch mode {
	case AggregationModeAlwaysOnline:
		return 3
	case AggregationModeTieredSummary:
		return 2
	case AggregationModeAdaptiveCache:
		return 1
	default:
		return 0
	}
}

func (p aggregateRuntimePolicy) usesTemplateWideSummary() bool {
	return p.Mode == AggregationModeTieredSummary || p.Mode == AggregationModeAlwaysOnline
}

func (p aggregateRuntimePolicy) forceValkeyAdmission() bool {
	return p.Mode == AggregationModeAdaptiveCache || p.Mode == AggregationModeAlwaysOnline
}
