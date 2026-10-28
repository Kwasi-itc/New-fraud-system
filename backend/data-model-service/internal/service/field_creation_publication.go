package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// PreparedFieldCreationRequestHash binds the retained allocated field identity
// as well as the canonical original command. This is not a pre-allocation key.
func PreparedFieldCreationRequestHash(scope PublicationRequestContext, fieldID uuid.UUID, input CreateFieldInput) ([32]byte, error) {
	if fieldID == uuid.Nil {
		return [32]byte{}, errors.New("retained field identity required")
	}
	command, err := encodePublicationRequest(scope, input)
	if err != nil {
		return [32]byte{}, err
	}
	raw, err := json.Marshal(struct {
		Format  string          `json:"format"`
		FieldID uuid.UUID       `json:"field_id"`
		Command json.RawMessage `json:"command"`
	}{"dm-prepared-field-creation/v1", fieldID, command})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// PublishPreparedCreation finalizes a previously allocated native field whose
// physical operation has executed. The table-bound manager must verify the full
// plan and metadata postcondition under its execution lock, and commit these
// owner effects with the contract, receipt and readiness. This returns the
// original publication on replay, not an original Field result. Stable field
// allocation/result recovery and enum/text-key protocols are separate contracts.
func (s FieldService) PublishPreparedCreation(ctx context.Context, tenantID, fieldID uuid.UUID, input CreateFieldInput, request ports.PhysicalPublicationRequest, manager ports.PhysicalPublicationManager) (contract.Publication, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil || tenantID == uuid.Nil || fieldID == uuid.Nil || request.OperationID == uuid.Nil || request.TableID != input.TableID || request.ActorKind != "api" || request.Condition.Mode != contract.PublicationExactVersion {
		return contract.Publication{}, errors.New("prepared field requires publisher, owner dependencies and exact original API command identity")
	}
	actor, err := uuid.Parse(request.ActorID)
	if err != nil || actor == uuid.Nil {
		return contract.Publication{}, errors.New("trusted API actor UUID required")
	}
	if err := datamodel.ValidateFieldCreate(input.Name, input.DataType, input.IsEnum, input.IsUnique); err != nil {
		return contract.Publication{}, err
	}
	if input.IsEnum || len(input.EnumValues) != 0 || (input.DataType == datamodel.DataTypeString && input.IsUnique) {
		return contract.Publication{}, errors.New("prepared native field cannot publish enum or unique-text enforcement")
	}
	switch input.DataType {
	case datamodel.DataTypeBool, datamodel.DataTypeInt, datamodel.DataTypeFloat, datamodel.DataTypeString, datamodel.DataTypeTimestamp, datamodel.DataTypeIPAddress:
	default:
		return contract.Publication{}, errors.New("unsupported native field type")
	}
	want, err := PreparedFieldCreationRequestHash(PublicationRequestContext{TenantID: tenantID, TableID: input.TableID, ActorID: actor, Condition: request.Condition}, fieldID, input)
	if err != nil {
		return contract.Publication{}, err
	}
	if want != request.RequestHash {
		return contract.Publication{}, errors.New("field request hash differs from original owner command")
	}
	return manager.Publish(ctx, request, func(store ports.MutationStore) error {
		tenantRecord, err := store.Tenants().GetByID(ctx, tenantID)
		if err != nil {
			return err
		}
		if tenantRecord.Status != tenant.StatusActive {
			return errors.New("tenant must remain active before publishing field creation")
		}
		table, err := store.Tables().GetByID(ctx, input.TableID)
		if err != nil {
			return err
		}
		if table.TenantID != tenantID || table.Archived {
			return errors.New("prepared field requires its active tenant table")
		}
		field, err := store.Fields().GetByID(ctx, fieldID)
		if err != nil {
			return err
		}
		if field.TenantID != tenantID || field.TableID != table.ID || field.Archived || field.Name != datamodel.NormalizeName(input.Name) || field.Description != input.Description || field.Alias != strings.TrimSpace(input.Alias) || field.DataType != input.DataType || field.Nullable != input.Nullable || field.IsUnique != input.IsUnique || field.IsEnum != input.IsEnum {
			return errors.New("prepared field differs from original creation command")
		}
		now := s.clock.Now()
		if err := recordSchemaChange(ctx, store.SchemaChanges(), s.idGenerator.New(), tenantID, "create_field", "field", field.ID, now, map[string]any{"table_id": field.TableID, "name": field.Name, "data_type": field.DataType, "nullable": field.Nullable, "is_enum": field.IsEnum, "is_unique": field.IsUnique, "description": field.Description}); err != nil {
			return err
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, tenantID, schemaMigrationVersion("create_field", "field"), now)
	})
}
