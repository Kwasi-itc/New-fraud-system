package ast_eval

import (
	"context"
	"fmt"
	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
)

// Filter groups describe SQL predicates. Record their compiled structure and
// inputs without inventing a per-row truth value on the incoming payload.
func aggregateFilterEvidence(ctx context.Context, n domainast.Node, r Runtime) (domainast.EvaluationNode, error) {
	switch canonicalFunctionName(n.Function) {
	case "list", "and", "or", "not":
		out := domainast.EvaluationNode{Function: n.Function, Constant: n.Constant, Children: make([]domainast.EvaluationNode, len(n.Children))}
		values := make([]any, len(n.Children))
		for i, child := range n.Children {
			v, err := aggregateFilterEvidence(ctx, child, r)
			if err != nil {
				return domainast.EvaluationNode{}, err
			}
			out.Children[i] = v
			values[i] = v.ReturnValue
		}
		if canonicalFunctionName(n.Function) == "list" {
			out.ReturnValue = values
			return out, nil
		}
		expr, supported, reason, err := parseAggregateFilterExpr(ctx, n, r)
		if err != nil {
			return domainast.EvaluationNode{}, err
		}
		if !supported {
			return domainast.EvaluationNode{}, fmt.Errorf("aggregate filter evidence unsupported: %s", reason)
		}
		out.ReturnValue = toAggregatePortFilter(expr)
		return out, nil
	default:
		return evaluateNodeWithEvidence(ctx, n, r)
	}
}
