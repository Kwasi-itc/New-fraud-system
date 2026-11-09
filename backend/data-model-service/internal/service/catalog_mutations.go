package service

import (
	"context"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// ApplyCatalogMutation runs inside the publisher's locked owner transaction.
// Existing record values are retained; enum membership governs future ingestion.
func (s FieldEnumValueService) ApplyCatalogMutation(ctx context.Context, tx ports.MutationStore, input UpdateFieldEnumValueInput, remove bool, tenantID, tableID uuid.UUID) (datamodel.FieldEnumValue, error) {
	value, err := tx.FieldEnumValues().GetByID(ctx, input.EnumValueID)
	if err != nil {
		return value, err
	}
	field, err := tx.Fields().GetByID(ctx, value.FieldID)
	if err != nil {
		return value, err
	}
	table, err := tx.Tables().GetByID(ctx, tableID)
	if err != nil {
		return value, err
	}
	t, err := tx.Tenants().GetByID(ctx, tenantID)
	if err != nil {
		return value, err
	}
	if value.TenantID != tenantID || field.TenantID != tenantID || field.TableID != tableID || table.Archived || field.Archived || !field.IsEnum || t.Status != tenant.StatusActive {
		return value, ErrFieldPatchInvalid
	}
	if input.SortOrder != nil && (*input.SortOrder < -2147483648 || *input.SortOrder > 2147483647) {
		return value, ErrFieldPatchInvalid
	}
	if err := datamodel.ValidateEnumValueUpdate(field, input.Value, input.Label); err != nil {
		return value, fmt.Errorf("%w: %v", ErrFieldPatchInvalid, err)
	}
	operation := "update_field_enum_value"
	now := s.clock.Now()
	if remove {
		operation = "delete_field_enum_value"
		err = tx.FieldEnumValues().Delete(ctx, value.ID)
	} else {
		if input.Value != nil {
			value.Value = *input.Value
		}
		if input.Label != nil {
			value.Label = *input.Label
		}
		if input.SortOrder != nil {
			value.SortOrder = *input.SortOrder
		}
		value.UpdatedAt = now
		err = tx.FieldEnumValues().Update(ctx, value)
	}
	if err != nil {
		return value, err
	}
	if err = recordSchemaChange(ctx, tx.SchemaChanges(), s.idGenerator.New(), tenantID, operation, "field_enum_value", value.ID, now, map[string]any{"field_id": value.FieldID, "value": value.Value, "label": value.Label, "sort_order": value.SortOrder}); err != nil {
		return value, err
	}
	err = recordTenantSchemaMigration(ctx, tx.TenantSchemaMigrations(), s.idGenerator, tenantID, schemaMigrationVersion(operation, "field_enum_value"), now)
	return value, err
}

type DeleteFieldInput struct{ FieldID uuid.UUID }

// ApplyPreparedDeletion is called only after verified physical retirement and
// dependency checks. The publisher owns removal of the physical registry rows.
func (s FieldService) ApplyPreparedDeletion(ctx context.Context, tx ports.MutationStore, input DeleteFieldInput, tenantID, tableID uuid.UUID) error {
	field, err := tx.Fields().GetByID(ctx, input.FieldID)
	if err != nil {
		return err
	}
	table, err := tx.Tables().GetByID(ctx, tableID)
	if err != nil {
		return err
	}
	t, err := tx.Tenants().GetByID(ctx, tenantID)
	if err != nil {
		return err
	}
	if field.TenantID != tenantID || field.TableID != tableID || table.Archived || t.Status != tenant.StatusActive || field.Name == "object_id" || field.Name == "updated_at" {
		return ErrFieldDependency
	}
	if err = tx.Fields().Delete(ctx, field.ID); err != nil {
		return err
	}
	now := s.clock.Now()
	if err = recordSchemaChange(ctx, tx.SchemaChanges(), s.idGenerator.New(), tenantID, "delete_field", "field", field.ID, now, map[string]any{"table_id": tableID, "name": field.Name, "data_type": field.DataType}); err != nil {
		return err
	}
	return recordTenantSchemaMigration(ctx, tx.TenantSchemaMigrations(), s.idGenerator, tenantID, schemaMigrationVersion("delete_field", "field"), now)
}

type DeleteEnumValueInput struct{ EnumValueID uuid.UUID }

// ApplyCatalogCreation validates and records a new value in the publisher's transaction.
func (s FieldEnumValueService) ApplyCatalogCreation(ctx context.Context, tx ports.MutationStore, input CreateFieldEnumValueInput, id, tenantID, tableID uuid.UUID) (datamodel.FieldEnumValue, error) {
	var value datamodel.FieldEnumValue
	field, err := tx.Fields().GetByID(ctx, input.FieldID)
	if err != nil {
		return value, err
	}
	table, err := tx.Tables().GetByID(ctx, tableID)
	if err != nil {
		return value, err
	}
	t, err := tx.Tenants().GetByID(ctx, tenantID)
	if err != nil {
		return value, err
	}
	if id == uuid.Nil || field.TenantID != tenantID || field.TableID != tableID || field.Archived || table.Archived || !field.IsEnum || t.Status != tenant.StatusActive {
		return value, ErrFieldPatchInvalid
	}
	if err = ValidateFieldSeeds(CreateFieldInput{Name: field.Name, DataType: field.DataType, IsEnum: true, EnumValues: []CreateFieldEnumValueSeed{{Value: input.Value, Label: input.Label, SortOrder: input.SortOrder}}}); err != nil {
		return value, fmt.Errorf("%w: %v", ErrFieldPatchInvalid, err)
	}
	now := s.clock.Now()
	value = datamodel.FieldEnumValue{ID: id, TenantID: tenantID, FieldID: field.ID, Value: input.Value, Label: input.Label, SortOrder: input.SortOrder, CreatedAt: now, UpdatedAt: now}
	if err = tx.FieldEnumValues().Create(ctx, value); err != nil {
		return value, err
	}
	if err = recordSchemaChange(ctx, tx.SchemaChanges(), s.idGenerator.New(), tenantID, "create_field_enum_value", "field_enum_value", id, now, map[string]any{"field_id": field.ID, "value": value.Value, "label": value.Label, "sort_order": value.SortOrder}); err != nil {
		return value, err
	}
	err = recordTenantSchemaMigration(ctx, tx.TenantSchemaMigrations(), s.idGenerator, tenantID, schemaMigrationVersion("create_field_enum_value", "field_enum_value"), now)
	return value, err
}
