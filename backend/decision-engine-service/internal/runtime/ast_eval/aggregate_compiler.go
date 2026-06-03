package ast_eval

import (
	"context"
	"fmt"
	"strings"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type AggregateCompileResult struct {
	Supported         bool
	Query             ports.AggregateQuery
	UnsupportedReason string
}

type aggregateFilterExpr struct {
	Kind     string
	Operator string
	Children []aggregateFilterExpr
	Field    string
	Op       string
	Value    any
}

func CompileAggregateQuery(ctx context.Context, node domainast.Node, runtime Runtime) (AggregateCompileResult, error) {
	tableName, err := evalNamedString(ctx, node, "tableName", runtime)
	if err != nil {
		return AggregateCompileResult{}, err
	}
	fieldName, err := evalNamedString(ctx, node, "fieldName", runtime)
	if err != nil {
		return AggregateCompileResult{}, err
	}
	aggregatorName, err := evalNamedString(ctx, node, "aggregator", runtime)
	if err != nil {
		return AggregateCompileResult{}, err
	}

	aggregateName := normalizeAggregateName(aggregatorName)
	switch aggregateName {
	case "count", "count_distinct", "sum", "avg", "min", "max":
	default:
		return AggregateCompileResult{
			UnsupportedReason: fmt.Sprintf("aggregate %q is not supported for pushdown", aggregatorName),
		}, nil
	}

	var compiledFilter *ports.AggregateFilter
	if filterNode, ok := node.NamedChildren["filters"]; ok {
		filterExpr, supported, reason, err := parseAggregateFilterExpr(ctx, filterNode, runtime)
		if err != nil {
			return AggregateCompileResult{}, err
		}
		if !supported {
			return AggregateCompileResult{UnsupportedReason: reason}, nil
		}
		filter := toAggregatePortFilter(filterExpr)
		compiledFilter = &filter
	}

	query := ports.AggregateQuery{
		ObjectType: tableName,
		Aggregate:  aggregateName,
		Field:      fieldName,
	}
	query.Filter = compiledFilter

	return AggregateCompileResult{
		Supported: true,
		Query:     query,
	}, nil
}

func parseAggregateFilterExpr(ctx context.Context, node domainast.Node, runtime Runtime) (aggregateFilterExpr, bool, string, error) {
	switch canonicalFunctionName(node.Function) {
	case "filter":
		fieldName, err := evalNamedString(ctx, node, "fieldName", runtime)
		if err != nil {
			return aggregateFilterExpr{}, false, "", err
		}
		operator, err := evalNamedString(ctx, node, "operator", runtime)
		if err != nil {
			return aggregateFilterExpr{}, false, "", err
		}
		var value any
		if valueNode, ok := node.NamedChildren["value"]; ok {
			resolved, err := EvaluateNode(ctx, valueNode, runtime)
			if err != nil {
				return aggregateFilterExpr{}, false, "", err
			}
			value = resolved
		}
		op, ok := normalizeAggregateFilterOperator(operator, value)
		if !ok {
			return aggregateFilterExpr{}, false, fmt.Sprintf("filter operator %q is not supported for pushdown", operator), nil
		}
		return aggregateFilterExpr{
			Kind:  "predicate",
			Field: fieldName,
			Op:    op,
			Value: value,
		}, true, "", nil
	case "list":
		children := make([]aggregateFilterExpr, 0, len(node.Children))
		for _, child := range node.Children {
			expr, supported, reason, err := parseAggregateFilterExpr(ctx, child, runtime)
			if err != nil {
				return aggregateFilterExpr{}, false, "", err
			}
			if !supported {
				return aggregateFilterExpr{}, false, reason, nil
			}
			children = append(children, expr)
		}
		return aggregateFilterExpr{
			Kind:     "group",
			Operator: "and",
			Children: children,
		}, true, "", nil
	case "and", "or":
		children := make([]aggregateFilterExpr, 0, len(node.Children))
		for _, child := range node.Children {
			expr, supported, reason, err := parseAggregateFilterExpr(ctx, child, runtime)
			if err != nil {
				return aggregateFilterExpr{}, false, "", err
			}
			if !supported {
				return aggregateFilterExpr{}, false, reason, nil
			}
			children = append(children, expr)
		}
		return aggregateFilterExpr{
			Kind:     "group",
			Operator: canonicalFunctionName(node.Function),
			Children: children,
		}, true, "", nil
	case "not":
		if len(node.Children) != 1 {
			return aggregateFilterExpr{}, false, "not filter expects exactly one child", nil
		}
		child, supported, reason, err := parseAggregateFilterExpr(ctx, node.Children[0], runtime)
		if err != nil {
			return aggregateFilterExpr{}, false, "", err
		}
		if !supported {
			return aggregateFilterExpr{}, false, reason, nil
		}
		return aggregateFilterExpr{
			Kind:     "group",
			Operator: "not",
			Children: []aggregateFilterExpr{child},
		}, true, "", nil
	default:
		return aggregateFilterExpr{}, false, fmt.Sprintf("filter node function %q is not supported for pushdown", node.Function), nil
	}
}

func toAggregatePortFilter(expr aggregateFilterExpr) ports.AggregateFilter {
	expr = normalizeAggregateFilterExpr(expr)
	out := ports.AggregateFilter{
		Kind:     expr.Kind,
		Operator: expr.Operator,
		Field:    expr.Field,
		Op:       expr.Op,
		Value:    expr.Value,
	}
	if len(expr.Children) > 0 {
		out.Children = make([]ports.AggregateFilter, len(expr.Children))
		for i, child := range expr.Children {
			out.Children[i] = toAggregatePortFilter(child)
		}
	}
	return out
}

func normalizeAggregateFilterExpr(expr aggregateFilterExpr) aggregateFilterExpr {
	for i, child := range expr.Children {
		expr.Children[i] = normalizeAggregateFilterExpr(child)
	}
	if strings.EqualFold(expr.Kind, "group") && strings.EqualFold(expr.Operator, "or") {
		if normalized, ok := collapseOrEqualsToIn(expr.Children); ok {
			return normalized
		}
	}
	return expr
}

func collapseOrEqualsToIn(children []aggregateFilterExpr) (aggregateFilterExpr, bool) {
	if len(children) < 2 {
		return aggregateFilterExpr{}, false
	}
	fieldName := ""
	values := make([]any, 0, len(children))
	for _, child := range children {
		if !strings.EqualFold(child.Kind, "predicate") {
			return aggregateFilterExpr{}, false
		}
		if child.Op != "eq" {
			return aggregateFilterExpr{}, false
		}
		if fieldName == "" {
			fieldName = child.Field
		} else if child.Field != fieldName {
			return aggregateFilterExpr{}, false
		}
		values = append(values, child.Value)
	}
	return aggregateFilterExpr{
		Kind:  "predicate",
		Field: fieldName,
		Op:    "in",
		Value: values,
	}, true
}

func normalizeAggregateName(name string) string {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "COUNT":
		return "count"
	case "COUNT_DISTINCT":
		return "count_distinct"
	case "SUM":
		return "sum"
	case "AVG":
		return "avg"
	case "MIN":
		return "min"
	case "MAX":
		return "max"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func normalizeAggregateFilterOperator(operator string, value any) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(operator)) {
	case "=", "eq":
		return "eq", true
	case "!=", "â‰ ", "neq":
		return "neq", true
	case ">", "gt":
		return "gt", true
	case ">=", "gte":
		return "gte", true
	case "<", "lt":
		return "lt", true
	case "<=", "lte":
		return "lte", true
	case "isinlist":
		if _, ok := value.([]any); !ok {
			return "", false
		}
		return "in", true
	case "stringstartswith":
		return "starts_with", true
	case "stringendswith":
		return "ends_with", true
	default:
		return "", false
	}
}
