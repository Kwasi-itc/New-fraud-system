package service

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// PublishPreparedCreation completes the owner phase of an already prepared and
// executed initial-table operation. The manager must bind the trusted actor and
// tenant/table, verify physical creation under its execution lock, and commit all
// effects and the receipt atomically. Preparation and original Table result
// receipts are separate: this method returns the original ingestion publication,
// never a fabricated current Table on historical replay. Existing Create and
// PostgreSQL runtime wiring are unchanged.
func (s TableService) PublishPreparedCreation(ctx context.Context, input CreateTableInput, request ports.PhysicalPublicationRequest, manager ports.PhysicalPublicationManager) (contract.Publication, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil || request.OperationID == uuid.Nil || request.ActorKind != "api" || request.Condition.Mode != contract.PublicationMustBeAbsent {
		return contract.Publication{}, errors.New("prepared creation requires publisher, owner dependencies and original API command identity")
	}
	actor, err := uuid.Parse(request.ActorID)
	if err != nil || actor == uuid.Nil {
		return contract.Publication{}, errors.New("trusted API actor UUID required")
	}
	if err := datamodel.ValidateTableCreate(input.Name); err != nil {
		return contract.Publication{}, err
	}
	if err := datamodel.ValidateSemanticType(input.SemanticType); err != nil {
		return contract.Publication{}, err
	}
	want, err := PublicationRequestHash(PublicationRequestContext{TenantID: input.TenantID, TableID: request.TableID, ActorID: actor, Condition: request.Condition}, input)
	if err != nil {
		return contract.Publication{}, err
	}
	if want != request.RequestHash {
		return contract.Publication{}, errors.New("creation request hash differs from original owner command")
	}
	return manager.Publish(ctx, request, func(store ports.MutationStore) error {
		tenantRecord, err := store.Tenants().GetByID(ctx, input.TenantID)
		if err != nil {
			return err
		}
		if tenantRecord.Status != tenant.StatusActive {
			return errors.New("tenant must remain active before publishing table creation")
		}
		table, err := store.Tables().GetByID(ctx, request.TableID)
		if err != nil {
			return err
		}
		if table.TenantID != input.TenantID || table.Archived || table.Name != datamodel.NormalizeName(input.Name) || table.Description != input.Description || table.Alias != input.Alias || table.SemanticType != datamodel.NormalizeName(input.SemanticType) || table.CaptionField != "" {
			return errors.New("prepared table differs from original creation command")
		}
		fields, err := store.Fields().ListByTable(ctx, table.ID)
		if err != nil {
			return err
		}
		if len(fields) != 2 {
			return errors.New("initial table requires exactly its two mandatory fields")
		}
		seen := map[string]bool{}
		for _, f := range fields {
			if f.TenantID != input.TenantID || f.TableID != table.ID || f.Archived || f.Nullable || f.IsEnum || seen[f.Name] {
				return errors.New("invalid prepared mandatory field")
			}
			seen[f.Name] = true
			switch f.Name {
			case "object_id":
				if f.DataType != datamodel.DataTypeString || !f.IsUnique {
					return errors.New("invalid prepared object_id")
				}
			case "updated_at":
				if f.DataType != datamodel.DataTypeTimestamp || f.IsUnique {
					return errors.New("invalid prepared updated_at")
				}
			default:
				return errors.New("unexpected initial field")
			}
		}
		now := s.clock.Now()
		if err := recordSchemaChange(ctx, store.SchemaChanges(), s.idGenerator.New(), input.TenantID, "create_table", "table", table.ID, now, map[string]any{"name": table.Name, "description": table.Description, "alias": table.Alias, "semantic_type": table.SemanticType, "default_fields": []string{"object_id", "updated_at"}}); err != nil {
			return err
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, input.TenantID, schemaMigrationVersion("create_table", "table"), now)
	})
}
