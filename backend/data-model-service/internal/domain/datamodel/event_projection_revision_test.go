package datamodel

import (
	"testing"

	"github.com/google/uuid"
)

func TestEventProjectionChangesSchemaRevision(t *testing.T) {
	table := AssembledTable{
		ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Name: "transactions",
		StorageClass: StorageClassEvent, EventTimeField: "date",
		Fields: map[string]AssembledField{
			"account_ref": {
				ID:   uuid.MustParse("22222222-2222-4222-8222-222222222222"),
				Name: "account_ref", DataType: DataTypeString,
			},
		},
	}
	withoutProjection := BuildEventSchemaRevision(table)
	field := table.Fields["account_ref"]
	field.IsProjection = true
	table.Fields["account_ref"] = field
	withProjection := BuildEventSchemaRevision(table)
	if withoutProjection == withProjection {
		t.Fatal("projection selection must change the immutable event schema revision")
	}
}

func TestEventAggregationPolicyDoesNotChangePhysicalSchemaRevision(t *testing.T) {
	table := AssembledTable{
		ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Name: "transactions",
		StorageClass: StorageClassEvent, EventTimeField: "date",
		Fields: map[string]AssembledField{
			"account_ref": {
				ID:   uuid.MustParse("22222222-2222-4222-8222-222222222222"),
				Name: "account_ref", DataType: DataTypeString, IsProjection: true,
				AggregationMode:         AggregationModeProjectionOnly,
				AggregationColdBehavior: AggregationColdQueryClickHouse,
			},
		},
	}
	before := BuildEventSchemaRevision(table)
	field := table.Fields["account_ref"]
	field.AggregationMode = AggregationModeAdaptiveCache
	field.AggregationColdBehavior = AggregationColdDeferAsync
	table.Fields["account_ref"] = field
	after := BuildEventSchemaRevision(table)
	if before != after {
		t.Fatal("runtime aggregation policy must not change the immutable ClickHouse schema revision")
	}
}
