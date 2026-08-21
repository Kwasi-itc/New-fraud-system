package ast_eval

import (
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestWithEventTimeUpperBoundUsesEvaluationTime(t *testing.T) {
	now := time.Date(2026, time.July, 1, 2, 3, 4, 0, time.UTC)
	lower := &ports.AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-06-01T00:00:00Z"}
	runtime := Runtime{Now: now, Model: &ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"transactions": {StorageClass: "event", EventTimeField: "date"},
	}}}

	filter := withEventTimeUpperBound(lower, "transactions", runtime)
	if filter == nil || filter.Operator != "and" || len(filter.Children) != 2 {
		t.Fatalf("upper-bound filter = %#v", filter)
	}
	upper := filter.Children[1]
	if upper.Field != "date" || upper.Op != "lte" || upper.Value != "2026-07-01T02:03:04Z" {
		t.Fatalf("upper bound = %#v", upper)
	}
}
