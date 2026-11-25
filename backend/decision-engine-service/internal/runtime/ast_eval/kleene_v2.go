package ast_eval

import (
	"context"
	"fmt"
	"slices"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
)

const (
	costTierPure       = 0
	costTierLocalDB    = 1
	costTierLookup     = 2
	costTierAggregator = 3
)

type logicalChildOrder struct {
	index int
	tier  int
}

type indexedEvalError struct {
	index int
	err   error
}

// ClassifyNodeCostTier returns the static AST cost tier (0-3) of node.
func ClassifyNodeCostTier(node domainast.Node) int {
	tier := functionBaseCostTier(canonicalFunctionName(node.Function))
	for _, child := range node.Children {
		if childTier := ClassifyNodeCostTier(child); childTier > tier {
			tier = childTier
		}
	}
	for _, child := range node.NamedChildren {
		if childTier := ClassifyNodeCostTier(child); childTier > tier {
			tier = childTier
		}
	}
	return tier
}

func functionBaseCostTier(canonical string) int {
	switch canonical {
	case "aggregator":
		return costTierAggregator
	case "related_field", "database_access", "past_decision_count", "related_count":
		return costTierLookup
	case "custom_list_access", "record_has_tag", "record_risk_level", "has_ip_flag",
		"ip_country", "ip_country_code", "ip_region", "ip_region_code", "ip_continent_code", "ip_geo_found":
		return costTierLocalDB
	default:
		return costTierPure
	}
}

func orderedLogicalChildren(children []domainast.Node) []logicalChildOrder {
	ordered := make([]logicalChildOrder, len(children))
	for i, child := range children {
		ordered[i] = logicalChildOrder{
			index: i,
			tier:  ClassifyNodeCostTier(child),
		}
	}
	slices.SortStableFunc(ordered, func(a, b logicalChildOrder) int {
		if a.tier != b.tier {
			if a.tier < b.tier {
				return -1
			}
			return 1
		}
		if a.index < b.index {
			return -1
		}
		if a.index > b.index {
			return 1
		}
		return 0
	})
	return ordered
}

func countTier3Nodes(node domainast.Node) int {
	count := 0
	if canonicalFunctionName(node.Function) == "aggregator" {
		count++
	}
	for _, child := range node.Children {
		count += countTier3Nodes(child)
	}
	for _, child := range node.NamedChildren {
		count += countTier3Nodes(child)
	}
	return count
}

func recordSkippedTier3Children(children []domainast.Node, visited []bool) {
	for i, child := range children {
		if visited[i] {
			continue
		}
		if tier3Count := countTier3Nodes(child); tier3Count > 0 {
			recordAggregateShortCircuitSkips(uint64(tier3Count))
		}
	}
}

func earliestAuthoredError(errs []indexedEvalError) error {
	if len(errs) == 0 {
		return nil
	}
	best := errs[0]
	for _, candidate := range errs[1:] {
		if candidate.index < best.index {
			best = candidate
		}
	}
	return best.err
}

func evaluateLogicalV2(ctx context.Context, op string, children []domainast.Node, runtime Runtime) (bool, error) {
	isAnd := op == "and"
	shortCircuitBool := !isAnd
	defaultBool := isAnd

	ordered := orderedLogicalChildren(children)
	visited := make([]bool, len(children))
	var deferredErrs []indexedEvalError

	for _, entry := range ordered {
		if len(deferredErrs) > 0 && entry.tier >= costTierAggregator {
			break
		}
		visited[entry.index] = true
		val, err := EvaluateNode(ctx, children[entry.index], runtime)
		if err != nil {
			deferredErrs = append(deferredErrs, indexedEvalError{index: entry.index, err: err})
			continue
		}
		boolVal, ok := val.(bool)
		if !ok {
			if nullErr := nullOperandError(op, fmt.Sprintf("child %d", entry.index+1), val); nullErr != nil {
				deferredErrs = append(deferredErrs, indexedEvalError{index: entry.index, err: nullErr})
			} else {
				deferredErrs = append(deferredErrs, indexedEvalError{
					index: entry.index,
					err:   fmt.Errorf("%s expects boolean children", op),
				})
			}
			continue
		}
		if boolVal == shortCircuitBool {
			recordSkippedTier3Children(children, visited)
			return shortCircuitBool, nil
		}
	}

	if len(deferredErrs) > 0 {
		return false, earliestAuthoredError(deferredErrs)
	}
	return defaultBool, nil
}

func evaluateLogicalWithEvidenceV2(
	ctx context.Context,
	evaluation domainast.EvaluationNode,
	op string,
	children []domainast.Node,
	runtime Runtime,
) (domainast.EvaluationNode, error) {
	isAnd := op == "and"
	shortCircuitBool := !isAnd
	defaultBool := isAnd

	evaluation.Children = make([]domainast.EvaluationNode, len(children))
	ordered := orderedLogicalChildren(children)
	visited := make([]bool, len(children))
	var deferredErrs []indexedEvalError

	for _, entry := range ordered {
		if len(deferredErrs) > 0 && entry.tier >= costTierAggregator {
			break
		}
		visited[entry.index] = true
		childEval, err := evaluateNodeWithEvidence(ctx, children[entry.index], runtime)
		if err != nil {
			deferredErrs = append(deferredErrs, indexedEvalError{index: entry.index, err: err})
			continue
		}
		evaluation.Children[entry.index] = childEval
		boolValue, ok := childEval.ReturnValue.(bool)
		if !ok {
			if nullErr := nullOperandError(op, fmt.Sprintf("child %d", entry.index+1), childEval.ReturnValue); nullErr != nil {
				deferredErrs = append(deferredErrs, indexedEvalError{index: entry.index, err: nullErr})
			} else {
				deferredErrs = append(deferredErrs, indexedEvalError{
					index: entry.index,
					err:   fmt.Errorf("%s expects boolean children", op),
				})
			}
			continue
		}
		if boolValue == shortCircuitBool {
			for i, child := range children {
				if !visited[i] {
					evaluation.Children[i] = skippedEvaluation(child)
				}
			}
			recordSkippedTier3Children(children, visited)
			evaluation.ReturnValue = shortCircuitBool
			return evaluation, nil
		}
	}

	if len(deferredErrs) > 0 {
		return domainast.EvaluationNode{}, earliestAuthoredError(deferredErrs)
	}
	evaluation.ReturnValue = defaultBool
	return evaluation, nil
}

// LintAggregatorWarnings returns non-blocking authoring warnings for Aggregator nodes.
func LintAggregatorWarnings(node domainast.Node) []string {
	var warnings []string
	walkASTNodes(node, func(current domainast.Node) {
		if canonicalFunctionName(current.Function) != "aggregator" {
			return
		}
		tableName, _ := current.NamedChildren["tableName"].Constant.(string)
		aggName, _ := current.NamedChildren["aggregator"].Constant.(string)
		filterNode, hasFilters := current.NamedChildren["filters"]

		hasEq := false
		hasBounded24h := false
		if hasFilters {
			hasEq, hasBounded24h = inspectAggregatorFilterForLint(filterNode)
		}
		if !hasEq {
			warnings = append(warnings, fmt.Sprintf("aggregator on table %q lacks an equality filter predicate", tableName))
		}
		if strings.EqualFold(strings.TrimSpace(aggName), "COUNT_DISTINCT") && !hasBounded24h {
			warnings = append(warnings, fmt.Sprintf("COUNT_DISTINCT aggregator on table %q uses a window exceeding 24h", tableName))
		}
	})
	return warnings
}

func walkASTNodes(node domainast.Node, visit func(domainast.Node)) {
	visit(node)
	for _, child := range node.Children {
		walkASTNodes(child, visit)
	}
	for _, child := range node.NamedChildren {
		walkASTNodes(child, visit)
	}
}

func inspectAggregatorFilterForLint(node domainast.Node) (hasEq bool, hasBounded24h bool) {
	switch canonicalFunctionName(node.Function) {
	case "list", "and":
		for _, child := range node.Children {
			eq, bounded := inspectAggregatorFilterForLint(child)
			if eq {
				hasEq = true
			}
			if bounded {
				hasBounded24h = true
			}
		}
		return hasEq, hasBounded24h
	case "filter":
		op, _ := node.NamedChildren["operator"].Constant.(string)
		op = strings.ToLower(strings.TrimSpace(op))
		if op == "eq" || op == "=" {
			hasEq = true
		}
		if op == "gt" || op == "gte" || op == ">" || op == ">=" {
			if valueNode, ok := node.NamedChildren["value"]; ok && isWithin24hTimeAdd(valueNode) {
				hasBounded24h = true
			}
		}
		return hasEq, hasBounded24h
	default:
		return false, false
	}
}

func isWithin24hTimeAdd(node domainast.Node) bool {
	if canonicalFunctionName(node.Function) != "time_add" {
		return false
	}
	amount, ok := asFloat64Lint(node.NamedChildren["amount"].Constant)
	if !ok {
		return false
	}
	unit, _ := node.NamedChildren["unit"].Constant.(string)
	unit = strings.ToLower(strings.TrimSpace(unit))
	if amount >= 0 {
		return true
	}
	switch unit {
	case "second", "seconds":
		return -amount <= 24*3600
	case "minute", "minutes":
		return -amount <= 24*60
	case "hour", "hours":
		return -amount <= 24
	case "day", "days":
		return -amount <= 1
	default:
		return false
	}
}

func asFloat64Lint(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
