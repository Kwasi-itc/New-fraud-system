package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
	"strings"
)

var ErrFieldDependency = errors.New("remove dependent relationships or enum values before changing the field constraint")
var ErrFieldDuplicate = errors.New("existing duplicate values prevent uniqueness")
var ErrFieldPatchInvalid = errors.New("invalid field constraint update")

func ValidatePhysicalFieldPatch(current datamodel.Field, input UpdateFieldInput) error {
	if current.Archived || current.ID != input.FieldID {
		return ErrFieldPatchInvalid
	}
	if err := datamodel.ValidateFieldUpdate(current, current.DataType, input.IsEnum, input.IsUnique, input.Nullable); err != nil {
		return fmt.Errorf("%w: %v", ErrFieldPatchInvalid, err)
	}
	return nil
}

// PublishPreparedConstraintsResult finalizes a verified physical plan. The manager
// verifies the complete postcondition and retains this response in the same
// transaction as the contract, audit and revision. Enum values use the same
// ingestion catalog validation as PostgreSQL, not a native MySQL ENUM column.
func (s FieldService) PublishPreparedConstraintsResult(ctx context.Context, tenantID uuid.UUID, input UpdateFieldInput, request ports.PhysicalPublicationRequest, manager ports.PhysicalFieldPublicationManager) (datamodel.Field, error) {
	if manager == nil || s.clock == nil || s.idGenerator == nil || request.ActorKind != "api" || request.Condition.Mode != contract.PublicationExactVersion {
		return datamodel.Field{}, errors.New("verified physical field publisher required")
	}
	actor, err := uuid.Parse(request.ActorID)
	if err != nil || actor == uuid.Nil {
		return datamodel.Field{}, errors.New("trusted actor required")
	}
	hash, err := PublicationRequestHash(PublicationRequestContext{TenantID: tenantID, TableID: request.TableID, ActorID: actor, Condition: request.Condition}, input)
	if err != nil {
		return datamodel.Field{}, err
	}
	if hash != request.RequestHash {
		return datamodel.Field{}, errors.New("field command hash mismatch")
	}
	return manager.PublishField(ctx, request, input.FieldID, func(store ports.MutationStore) error {
		t, err := store.Tenants().GetByID(ctx, tenantID)
		if err != nil {
			return err
		}
		table, err := store.Tables().GetByID(ctx, request.TableID)
		if err != nil {
			return err
		}
		if t.Status != tenant.StatusActive || table.TenantID != tenantID || table.Archived {
			return ErrFieldPatchInvalid
		}
		field, err := store.Fields().GetByID(ctx, input.FieldID)
		if err != nil {
			return err
		}
		if field.TenantID != tenantID || field.TableID != table.ID {
			return ErrFieldPatchInvalid
		}
		if err := ValidatePhysicalFieldPatch(field, input); err != nil {
			return err
		}
		if input.IsEnum != nil && !*input.IsEnum && field.IsEnum {
			values, err := store.FieldEnumValues().ListByField(ctx, field.ID)
			if err != nil {
				return err
			}
			if len(values) > 0 {
				return ErrFieldDependency
			}
		}
		if input.Nullable != nil {
			field.Nullable = *input.Nullable
		}
		if input.IsUnique != nil {
			field.IsUnique = *input.IsUnique
		}
		if input.IsEnum != nil {
			field.IsEnum = *input.IsEnum
		}
		if input.Alias != nil {
			field.Alias = strings.TrimSpace(*input.Alias)
		}
		if input.Description != nil {
			field.Description = *input.Description
		}
		field.UpdatedAt = s.clock.Now()
		if err := store.Fields().Update(ctx, field); err != nil {
			return err
		}
		if err := recordSchemaChange(ctx, store.SchemaChanges(), s.idGenerator.New(), tenantID, "update_field", "field", field.ID, field.UpdatedAt, map[string]any{"table_id": field.TableID, "nullable": field.Nullable, "is_unique": field.IsUnique, "is_enum": field.IsEnum}); err != nil {
			return err
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, tenantID, schemaMigrationVersion("update_field", "field"), field.UpdatedAt)
	})
}
