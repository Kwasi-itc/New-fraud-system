package redisfacts

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

func TestBackfillQueryUsesDefinitionColumnsAndGroupedBuckets(t *testing.T) {
	definition := ports.AggregateFactDefinition{
		TableName:       "transactions",
		DimensionFields: []string{"merchant_id"},
		EventTimeField:  "date",
		MeasureField:    "amount",
		NeedsSum:        true,
		NeedsCount:      true,
	}
	query := backfillQuery(uuid.MustParse("4a63be41-8628-4176-8d70-b8615e671068"), definition)
	for _, expected := range []string{
		`"tenant_4a63be41862841768d70b8615e671068"."transactions"`,
		`extract(epoch FROM "date") / 3600`,
		`"merchant_id" IS NOT NULL`,
		`COALESCE(SUM("amount"), 0)::double precision`,
		`COUNT("amount")::bigint`,
		`GROUP BY 1, 2`,
		`'hour'::text AS fact_resolution`,
		`'day'::text AS fact_resolution`,
		`WITH base AS MATERIALIZED`,
	} {
		if !strings.Contains(query, expected) {
			t.Fatalf("query does not contain %q:\n%s", expected, query)
		}
	}
}

func TestSameDefinitionGenerationsRejectsVersionChanges(t *testing.T) {
	left := []ports.AggregateFactDefinition{{
		ID: "definition-1", TableName: "transactions", Version: 1,
		CoverageToken: "coverage-1", RegistryVersion: 1,
	}}
	right := append([]ports.AggregateFactDefinition(nil), left...)
	if !sameDefinitionGenerations(left, right) {
		t.Fatal("identical generations were rejected")
	}
	right[0].Version = 2
	if sameDefinitionGenerations(left, right) {
		t.Fatal("version change was not detected")
	}
}
