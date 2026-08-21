package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

func TestEventTableAndFieldMutationsRejectLockedSchema(t *testing.T) {
	lockedAt := time.Now().UTC()
	tenantID := uuid.New()
	tableID := uuid.New()
	table := datamodel.Table{
		ID: tableID, TenantID: tenantID, Name: "transactions",
		StorageClass: datamodel.StorageClassEvent, EventSchemaLockedAt: &lockedAt,
	}

	tableService := NewTableService(
		stubTenantRepository{}, stubTableRepository{table: table}, stubFieldRepository{},
		stubLinkRepository{}, &stubPivotRepository{}, &stubSchemaChangeRepository{}, nil,
		stubTransactionManager{}, stubIDGenerator{value: uuid.New()}, stubClock{now: lockedAt},
	)
	description := "changed"
	if _, err := tableService.Update(context.Background(), UpdateTableInput{TableID: tableID, Description: &description}); !errors.Is(err, datamodel.ErrEventSchemaLocked) {
		t.Fatalf("Update() error = %v, want ErrEventSchemaLocked", err)
	}

	fieldService := NewFieldService(
		stubTenantRepository{}, stubTableRepository{table: table}, stubFieldRepository{}, nil,
		stubLinkRepository{}, &stubPivotRepository{}, &stubSchemaChangeRepository{}, nil,
		stubTransactionManager{}, stubIDGenerator{value: uuid.New()}, stubClock{now: lockedAt},
	)
	if _, err := fieldService.Create(context.Background(), CreateFieldInput{TableID: tableID, Name: "amount", DataType: datamodel.DataTypeFloat}); !errors.Is(err, datamodel.ErrEventSchemaLocked) {
		t.Fatalf("Create() error = %v, want ErrEventSchemaLocked", err)
	}
}

func TestLockedEventFieldAllowsRuntimeAggregationPolicyUpdate(t *testing.T) {
	lockedAt := time.Now().UTC()
	tenantID := uuid.New()
	tableID := uuid.New()
	fieldID := uuid.New()
	table := datamodel.Table{
		ID: tableID, TenantID: tenantID, Name: "transactions",
		StorageClass: datamodel.StorageClassEvent, EventSchemaLockedAt: &lockedAt,
	}
	fieldRepo := fieldRepositoryByTableAndID{byID: map[uuid.UUID]datamodel.Field{
		fieldID: {
			ID: fieldID, TenantID: tenantID, TableID: tableID, Name: "account_ref",
			DataType: datamodel.DataTypeString, IsProjection: true,
			AggregationMode:         datamodel.AggregationModeProjectionOnly,
			AggregationColdBehavior: datamodel.AggregationColdQueryClickHouse,
		},
	}}
	changes := &stubSchemaChangeRepository{}
	fieldService := NewFieldService(
		stubTenantRepository{}, stubTableRepository{table: table}, fieldRepo, nil,
		stubLinkRepository{}, &stubPivotRepository{}, changes, nil,
		stubTransactionManager{store: stubMutationStore{fields: fieldRepo, schemaChanges: changes}},
		stubIDGenerator{value: uuid.New()}, stubClock{now: lockedAt.Add(time.Minute)},
	)
	mode := datamodel.AggregationModeAdaptiveCache
	cold := datamodel.AggregationColdDeferAsync
	updated, err := fieldService.Update(context.Background(), UpdateFieldInput{
		FieldID: fieldID, AggregationMode: &mode, AggregationColdBehavior: &cold,
	})
	if err != nil {
		t.Fatalf("Update() error = %v; runtime policy must remain mutable after the physical schema is locked", err)
	}
	if updated.AggregationMode != mode || updated.AggregationColdBehavior != cold {
		t.Fatalf("updated policy = %s/%s, want %s/%s", updated.AggregationMode, updated.AggregationColdBehavior, mode, cold)
	}
	if len(changes.changes) != 1 || changes.changes[0].Operation != "update_field" {
		t.Fatalf("schema changes = %#v, want one audited update_field operation", changes.changes)
	}
}
