package ast_eval

import (
	"context"
	"fmt"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
)

// AggregateTemplateValue supplies a statically checked literal or a typed witness
// for a dynamic value. A witness is used ONLY to validate compilation: it is never
// an evaluated result or a replacement for runtime value validation. No runtime
// payload, related-record lookup or external function is evaluated here.
type AggregateTemplateValue func(table, field string, value domainast.Node) (any, error)

// CompileAggregateTemplate uses the exact runtime compiler/operator normalization
// after resolving constant identifiers and replacing values with static witnesses.
// Dynamic identifiers and cross-table filters fail closed before publication.
func CompileAggregateTemplate(ctx context.Context, node domainast.Node, resolve AggregateTemplateValue) (AggregateCompileResult, error) {
	if canonicalFunctionName(node.Function) != "aggregator" || resolve == nil {
		return AggregateCompileResult{}, fmt.Errorf("aggregate template and value resolver required")
	}
	copyNode := node
	copyNode.NamedChildren = make(map[string]domainast.Node, len(node.NamedChildren))
	for key, child := range node.NamedChildren {
		copyNode.NamedChildren[key] = child
	}
	for _, key := range []string{"tableName", "fieldName", "aggregator"} {
		value, ok := constantStringNode(node.NamedChildren[key])
		if !ok || value == "" {
			return AggregateCompileResult{}, fmt.Errorf("aggregate %s must be a constant identifier", key)
		}
		copyNode.NamedChildren[key] = domainast.Node{Constant: value}
	}
	table, _ := constantStringNode(copyNode.NamedChildren["tableName"])
	nodes := 0
	var filter func(domainast.Node, int) (domainast.Node, error)
	filter = func(n domainast.Node, depth int) (domainast.Node, error) {
		nodes++
		if depth > 5 || nodes > 50 || len(n.Children) > 20 {
			return domainast.Node{}, fmt.Errorf("aggregate filter exceeds depth/node/child limits")
		}
		out := domainast.Node{Function: n.Function}
		switch canonicalFunctionName(n.Function) {
		case "filter":
			if len(n.Children) != 0 {
				return out, fmt.Errorf("filter cannot have positional children")
			}
			out.NamedChildren = make(map[string]domainast.Node)
			for _, key := range []string{"tableName", "fieldName", "operator"} {
				value, ok := constantStringNode(n.NamedChildren[key])
				if !ok || value == "" || (key == "tableName" && value != table) {
					return out, fmt.Errorf("filter %s must bind the aggregate's table/field/operator", key)
				}
				out.NamedChildren[key] = domainast.Node{Constant: value}
			}
			field, _ := constantStringNode(out.NamedChildren["fieldName"])
			value := any(nil)
			if child, ok := n.NamedChildren["value"]; ok {
				var err error
				value, err = resolve(table, field, child)
				if err != nil {
					return out, err
				}
			}
			out.NamedChildren["value"] = domainast.Node{Constant: value}
			return out, nil
		case "list", "and", "or", "not":
			out.Children = make([]domainast.Node, len(n.Children))
			for i, child := range n.Children {
				var err error
				out.Children[i], err = filter(child, depth+1)
				if err != nil {
					return out, err
				}
			}
			return out, nil
		default:
			return out, fmt.Errorf("unsupported aggregate filter node %q", n.Function)
		}
	}
	if n, ok := node.NamedChildren["filters"]; ok {
		resolved, err := filter(n, 1)
		if err != nil {
			return AggregateCompileResult{}, err
		}
		copyNode.NamedChildren["filters"] = resolved
	}
	return CompileAggregateQuery(ctx, copyNode, Runtime{})
}
