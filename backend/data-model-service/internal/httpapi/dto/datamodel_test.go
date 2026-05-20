package dto

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

func TestAdaptAssembledDataModelPublishesIngestionContract(t *testing.T) {
	t.Parallel()

	model := datamodel.AssembledDataModel{
		Tables: map[string]datamodel.AssembledTable{
			"transactions": {
				ID:           uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				Name:         "transactions",
				Description:  "Transaction records",
				Alias:        "Transactions",
				SemanticType: "event",
				CaptionField: "object_id",
				Archived:     false,
				Fields: map[string]datamodel.AssembledField{
					"status": {
						ID:          uuid.MustParse("22222222-2222-2222-2222-222222222222"),
						Name:        "status",
						Description: "Transaction status",
						DataType:    datamodel.DataTypeString,
						Nullable:    false,
						IsEnum:      true,
						IsUnique:    false,
						Archived:    false,
						EnumValues: []datamodel.FieldEnumValue{
							{
								ID:    uuid.MustParse("33333333-3333-3333-3333-333333333333"),
								Value: "pending",
								Label: "Pending",
							},
						},
					},
				},
				LinksToSingle:     map[string]datamodel.AssembledLink{},
				NavigationOptions: []datamodel.NavigationOption{},
			},
		},
		Pivots: []datamodel.AssembledPivot{},
	}

	response := AdaptAssembledDataModel(model, "rev_123", tenant.StatusActive)

	if response.RevisionID != "rev_123" {
		t.Fatalf("expected revision id rev_123, got %s", response.RevisionID)
	}
	if response.IngestionContract.TenantStatus != "active" {
		t.Fatalf("expected active tenant status, got %s", response.IngestionContract.TenantStatus)
	}
	if !response.IngestionContract.Writable {
		t.Fatal("expected active tenant to be writable")
	}
	if response.IngestionContract.RecordLookupField != "object_id" {
		t.Fatalf("expected object_id lookup field, got %s", response.IngestionContract.RecordLookupField)
	}
	if !response.IngestionContract.PartialUpdates {
		t.Fatal("expected partial updates to be enabled")
	}
	expectedManagedFields := []string{"object_id", "updated_at", "valid_from", "valid_until"}
	if len(response.IngestionContract.ManagedSystemFields) != len(expectedManagedFields) {
		t.Fatalf("unexpected managed system field count: %v", response.IngestionContract.ManagedSystemFields)
	}
	for i, fieldName := range expectedManagedFields {
		if response.IngestionContract.ManagedSystemFields[i] != fieldName {
			t.Fatalf("unexpected managed system fields ordering: %v", response.IngestionContract.ManagedSystemFields)
		}
	}

	table := response.Tables["transactions"]
	if table.Archived {
		t.Fatal("expected transactions table archived=false")
	}
	field := table.Fields["status"]
	if field.Archived {
		t.Fatal("expected status field archived=false")
	}
	if len(field.EnumValues) != 1 || field.EnumValues[0].Value != "pending" {
		t.Fatalf("expected enum values to be preserved, got %+v", field.EnumValues)
	}
}
