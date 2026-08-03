package ingestion

import (
	"fmt"
	"sort"
	"strings"
)

type AggregateQuery struct {
	ObjectType string           `json:"object_type"`
	Aggregate  string           `json:"aggregate"`
	Field      string           `json:"field,omitempty"`
	Filter     *AggregateFilter `json:"filter,omitempty"`
}

type AggregateFilter struct {
	Kind     string            `json:"kind,omitempty"`
	Operator string            `json:"operator,omitempty"`
	Children []AggregateFilter `json:"children,omitempty"`
	Field    string            `json:"field,omitempty"`
	Op       string            `json:"op,omitempty"`
	Value    any               `json:"value,omitempty"`
}

const (
	AggregateFilterKindGroup     = "group"
	AggregateFilterKindPredicate = "predicate"
)

const (
	MaxAggregateFilterDepth   = 5
	MaxAggregateFilterNodes   = 50
	MaxAggregateGroupChildren = 20
	MaxAggregateInListItems   = 100
)

var aggregateIndexFriendlyGroupOperators = map[string]struct{}{
	"and": {},
	"or":  {},
	"not": {},
}

var aggregateIndexFriendlyPredicateOperators = map[string]struct{}{
	"eq":          {},
	"neq":         {},
	"gt":          {},
	"gte":         {},
	"lt":          {},
	"lte":         {},
	"in":          {},
	"is_null":     {},
	"is_not_null": {},
	"starts_with": {},
}

type AggregateQueryShape struct {
	FilterDepth int
	FilterNodes int
}

func NormalizedAggregateQueryShapeKey(query AggregateQuery) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(query.ObjectType)),
		strings.ToLower(strings.TrimSpace(query.Aggregate)),
		strings.ToLower(strings.TrimSpace(query.Field)),
	}
	if query.Filter == nil {
		return strings.Join(parts, "|")
	}
	return strings.Join(append(parts, normalizeAggregateFilterShape(*query.Filter)), "|")
}

func ValidateAggregateQuery(query AggregateQuery) error {
	if strings.TrimSpace(query.ObjectType) == "" {
		return fmt.Errorf("object_type is required")
	}
	if strings.TrimSpace(query.Aggregate) == "" {
		return fmt.Errorf("aggregate is required")
	}
	if query.Filter == nil {
		return nil
	}

	stats := aggregateFilterStats{}
	if err := validateAggregateFilter(*query.Filter, 1, &stats); err != nil {
		return err
	}
	if stats.nodes > MaxAggregateFilterNodes {
		return fmt.Errorf("aggregate filter exceeds the maximum supported size of %d nodes", MaxAggregateFilterNodes)
	}
	return nil
}

func MeasureAggregateQueryShape(query AggregateQuery) AggregateQueryShape {
	if query.Filter == nil {
		return AggregateQueryShape{}
	}
	stats := aggregateFilterStats{}
	measureAggregateFilter(*query.Filter, 1, &stats)
	return AggregateQueryShape{
		FilterDepth: stats.maxDepth,
		FilterNodes: stats.nodes,
	}
}

type aggregateFilterStats struct {
	nodes    int
	maxDepth int
}

func validateAggregateFilter(filter AggregateFilter, depth int, stats *aggregateFilterStats) error {
	if depth > MaxAggregateFilterDepth {
		return fmt.Errorf("aggregate filter exceeds the maximum supported depth of %d", MaxAggregateFilterDepth)
	}
	measureAggregateFilter(filter, depth, stats)

	kind := strings.ToLower(strings.TrimSpace(filter.Kind))
	if kind == "" {
		kind = AggregateFilterKindGroup
	}
	switch kind {
	case AggregateFilterKindGroup:
		op := strings.ToLower(strings.TrimSpace(filter.Operator))
		if op == "" {
			op = "and"
		}
		if _, ok := aggregateIndexFriendlyGroupOperators[op]; !ok {
			return fmt.Errorf("aggregate filter group operator %q is not supported for pushdown", filter.Operator)
		}
		if len(filter.Children) > MaxAggregateGroupChildren {
			return fmt.Errorf("aggregate filter group exceeds the maximum supported child count of %d", MaxAggregateGroupChildren)
		}
		if op == "not" && len(filter.Children) != 1 {
			return fmt.Errorf("aggregate not filter expects exactly one child")
		}
		for _, child := range filter.Children {
			if err := validateAggregateFilter(child, depth+1, stats); err != nil {
				return err
			}
		}
	case AggregateFilterKindPredicate:
		op := strings.ToLower(strings.TrimSpace(filter.Op))
		if _, ok := aggregateIndexFriendlyPredicateOperators[op]; !ok {
			return fmt.Errorf("aggregate filter operator %q is not supported for pushdown", filter.Op)
		}
		if op == "in" {
			items, ok := filter.Value.([]any)
			if !ok {
				return fmt.Errorf("aggregate in filter expects a list value")
			}
			if len(items) > MaxAggregateInListItems {
				return fmt.Errorf("aggregate in filter exceeds the maximum supported size of %d items", MaxAggregateInListItems)
			}
		}
	default:
		return fmt.Errorf("filter kind %q is not supported", filter.Kind)
	}

	return nil
}

func measureAggregateFilter(filter AggregateFilter, depth int, stats *aggregateFilterStats) {
	stats.nodes++
	if depth > stats.maxDepth {
		stats.maxDepth = depth
	}
	for _, child := range filter.Children {
		measureAggregateFilter(child, depth+1, stats)
	}
}

func normalizeAggregateFilterShape(filter AggregateFilter) string {
	kind := strings.ToLower(strings.TrimSpace(filter.Kind))
	if kind == "" {
		kind = AggregateFilterKindGroup
	}
	switch kind {
	case AggregateFilterKindPredicate:
		return "predicate:" +
			strings.ToLower(strings.TrimSpace(filter.Field)) + ":" +
			strings.ToLower(strings.TrimSpace(filter.Op))
	default:
		op := strings.ToLower(strings.TrimSpace(filter.Operator))
		if op == "" {
			op = "and"
		}
		children := make([]string, 0, len(filter.Children))
		for _, child := range filter.Children {
			children = append(children, normalizeAggregateFilterShape(child))
		}
		sort.Strings(children)
		return "group:" + op + "(" + strings.Join(children, ",") + ")"
	}
}
