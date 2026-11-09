package ast_eval

import (
	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

// Aggregate boolean groups describe predicates, not boolean-valued Filter
// objects. Validate in that context without weakening ordinary and/or typing.
func validateAggregateFilterTree(n domainast.Node, model ports.TenantModel, table string, depth int, nodes *int) []string {
	*nodes++
	if depth > 5 || *nodes > 50 {
		return []string{"aggregate filter exceeds depth/node limit"}
	}
	switch canonicalFunctionName(n.Function) {
	case "filter":
		_, issues := ValidateNode(n, model, table)
		return issues
	case "list", "and", "or", "not":
		if len(n.NamedChildren) != 0 || len(n.Children) > 20 {
			return []string{"invalid aggregate filter group"}
		}
		if canonicalFunctionName(n.Function) == "not" && len(n.Children) != 1 {
			return []string{"not filter requires exactly one child"}
		}
		var issues []string
		for _, child := range n.Children {
			issues = append(issues, validateAggregateFilterTree(child, model, table, depth+1, nodes)...)
		}
		return issues
	default:
		// Preserve legacy list-valued expressions; strict compilation still
		// rejects any expression it cannot translate safely.
		kind, issues := ValidateNode(n, model, table)
		if kind != domainast.ValueTypeList {
			issues = append(issues, "aggregate filters require predicates, groups or a list-valued expression")
		}
		return issues
	}
}
