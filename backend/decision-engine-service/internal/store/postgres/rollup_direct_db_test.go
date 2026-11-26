package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestDirectDBRollupEligibilityAndUnionSQL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 26, 9, 10, 0, 0, time.UTC)
	model := ports.TenantModel{
		RevisionID:        "rev-1",
		RecordLookupField: "object_id",
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"object_id":   {Name: "object_id", Type: "string"},
					"merchant_id": {Name: "merchant_id", Type: "string"},
					"amount":      {Name: "amount", Type: "int"},
					"created_at":  {Name: "created_at", Type: "timestamptz"},
				},
			},
		},
	}
	defs := []directDBRollupDef{
		{
			ID:           "11111111-1111-4111-8111-111111111111",
			TableName:    "transactions",
			GroupField:   "merchant_id",
			TimeField:    "created_at",
			ValueField:   "amount",
			ValueType:    "int",
			ModelVersion: "rev-1",
			Status:       "ready",
		},
	}
	query := ports.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "sum",
		Field:      "amount",
		Filter: &ports.AggregateFilter{
			Kind:     "group",
			Operator: "and",
			Children: []ports.AggregateFilter{
				{Kind: "predicate", Field: "merchant_id", Op: "eq", Value: "m1"},
				{Kind: "predicate", Field: "created_at", Op: "gte", Value: now.Add(-24 * time.Hour)},
			},
		},
	}

	cand, reason := checkDirectDBRollupEligibility(model, model.Tables["transactions"], query, defs, now)
	if reason != "" {
		t.Fatalf("checkDirectDBRollupEligibility() reason = %q, want empty", reason)
	}
	if len(cand.Plan.RawEdges) != 1 {
		t.Fatalf("expected 1 lower raw edge (and zero upper raw edge for unbounded above), got %d", len(cand.Plan.RawEdges))
	}
	sqlStr, _, err := buildDirectDBRollupUnionSQL("22222222-2222-4222-8222-222222222222", "tenant_2222", model.Tables["transactions"], cand)
	if err != nil {
		t.Fatalf("buildDirectDBRollupUnionSQL() error = %v", err)
	}
	for _, want := range []string{
		"core_ingestion.rollup_buckets",
		"core_ingestion.rollup_deltas",
		"COALESCE(SUM(s), 0)::bigint",
		"UNION ALL",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("expected %q in SQL:\n%s", want, sqlStr)
		}
	}
}
