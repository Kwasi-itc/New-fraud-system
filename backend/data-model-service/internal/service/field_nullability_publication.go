package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// PublishPreparedNullabilityUpdate finalizes a durable native nullability ALTER.
// The physical manager must hold verified shape, journal, grants and readiness
// through this owner transaction and check its complete metadata postcondition.
// It returns the retained publication; original Field response recovery is separate.
func (s FieldService) PublishPreparedNullabilityUpdate(ctx context.Context, tenantID uuid.UUID, input UpdateFieldInput, request ports.PhysicalPublicationRequest, manager ports.PhysicalPublicationManager) (contract.Publication, error) {
	if input.Nullable == nil || input.IsUnique != nil || input.IsEnum != nil {
		return contract.Publication{}, errors.New("native nullability command required")
	}
	return s.publishPreparedFieldUpdate(ctx, tenantID, input, request, manager, false)
}

// PublishPreparedUniqueEnable finalizes a native numeric unique index. Removing
// uniqueness requires relationship checks in its own preparation protocol; text
// keys and enums also have separate enforcement protocols.
func (s FieldService) PublishPreparedUniqueEnable(ctx context.Context, tenantID uuid.UUID, input UpdateFieldInput, request ports.PhysicalPublicationRequest, manager ports.PhysicalPublicationManager) (contract.Publication, error) {
	if input.IsUnique == nil || !*input.IsUnique || input.Nullable != nil || input.IsEnum != nil {
		return contract.Publication{}, errors.New("native uniqueness enable command required")
	}
	return s.publishPreparedFieldUpdate(ctx, tenantID, input, request, manager, true)
}

func (s FieldService) publishPreparedFieldUpdate(ctx context.Context, tenantID uuid.UUID, input UpdateFieldInput, request ports.PhysicalPublicationRequest, manager ports.PhysicalPublicationManager, enableUnique bool) (contract.Publication, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil || tenantID == uuid.Nil || request.TableID == uuid.Nil || request.OperationID == uuid.Nil || request.ActorKind != "api" || request.Condition.Mode != contract.PublicationExactVersion {
		return contract.Publication{}, errors.New("native field publication requires exact API command and owner dependencies")
	}
	actor, err := uuid.Parse(request.ActorID)
	if err != nil || actor == uuid.Nil {
		return contract.Publication{}, errors.New("trusted API actor UUID required")
	}
	hash, err := PublicationRequestHash(PublicationRequestContext{TenantID: tenantID, TableID: request.TableID, ActorID: actor, Condition: request.Condition}, input)
	if err != nil {
		return contract.Publication{}, err
	}
	if hash != request.RequestHash {
		return contract.Publication{}, errors.New("native field command differs from canonical request")
	}
	return manager.Publish(ctx, request, func(store ports.MutationStore) error {
		t, err := store.Tenants().GetByID(ctx, tenantID)
		if err != nil {
			return err
		}
		if t.Status != tenant.StatusActive {
			return errors.New("active tenant required")
		}
		table, err := store.Tables().GetByID(ctx, request.TableID)
		if err != nil {
			return err
		}
		if table.TenantID != tenantID || table.Archived {
			return errors.New("active tenant-owned table required")
		}
		field, err := store.Fields().GetByID(ctx, input.FieldID)
		if err != nil {
			return err
		}
		if field.TenantID != tenantID || field.TableID != request.TableID || field.Archived || field.IsEnum {
			return errors.New("active non-enum field in publication table required")
		}
		if err := datamodel.ValidateFieldUpdate(field, field.DataType, nil, input.IsUnique, input.Nullable); err != nil {
			return err
		}
		if enableUnique {
			if field.DataType != datamodel.DataTypeInt && field.DataType != datamodel.DataTypeFloat {
				return errors.New("native uniqueness owner supports only integer and float fields")
			}
			field.IsUnique = true
		} else {
			field.Nullable = *input.Nullable
		}
		if input.Description != nil {
			field.Description = *input.Description
		}
		if input.Alias != nil {
			field.Alias = strings.TrimSpace(*input.Alias)
		}
		field.UpdatedAt = s.clock.Now()
		if err := store.Fields().Update(ctx, field); err != nil {
			return err
		}
		if err := recordSchemaChange(ctx, store.SchemaChanges(), s.idGenerator.New(), tenantID, "update_field", "field", field.ID, field.UpdatedAt, map[string]any{"table_id": field.TableID, "name": field.Name, "nullable": field.Nullable, "is_enum": field.IsEnum, "is_unique": field.IsUnique, "description": field.Description}); err != nil {
			return err
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, tenantID, schemaMigrationVersion("update_field", "field"), field.UpdatedAt)
	})
}
