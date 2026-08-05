package ast_eval

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func aggregateQueryShapeKey(query ports.AggregateQuery) string {
	return fmt.Sprintf("%s|%s|%s|%s",
		strings.TrimSpace(query.ObjectType),
		strings.ToLower(strings.TrimSpace(query.Aggregate)),
		strings.TrimSpace(query.Field),
		aggregateFilterShape(query.Filter),
	)
}

func aggregateFilterShape(filter *ports.AggregateFilter) string {
	if filter == nil {
		return "none"
	}
	if len(filter.Children) > 0 {
		childShapes := make([]string, 0, len(filter.Children))
		for _, child := range filter.Children {
			child := child
			childShapes = append(childShapes, aggregateFilterShape(&child))
		}
		return fmt.Sprintf("group:%s(%s)", strings.ToLower(strings.TrimSpace(filter.Operator)), strings.Join(childShapes, ","))
	}
	return fmt.Sprintf("pred:%s:%s", strings.TrimSpace(filter.Field), strings.ToLower(strings.TrimSpace(filter.Op)))
}
