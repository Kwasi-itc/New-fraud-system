package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

// PublishPresentationUpdate publishes field description/alias edits atomically
// with their audit, tenant revision and replay receipt. Enforcement changes use
// the physical publication protocol. The result is a publication, not a retained
// historical Field response.
func (s FieldService) PublishPresentationUpdate(ctx context.Context, input UpdateFieldInput, request ports.MetadataPublicationRequest, manager ports.MetadataPublicationManager) (contract.Publication, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil || input.Nullable != nil || input.IsUnique != nil || input.IsEnum != nil {
		return contract.Publication{}, errors.New("presentation publisher and owner dependencies required; enforcement changes use physical publication")
	}
	hash, err := PublicationRequestHash(PublicationRequestContext{TenantID: request.TenantID, TableID: request.TableID, ActorID: request.ActorID, Condition: request.Condition}, input)
	if err != nil {
		return contract.Publication{}, err
	}
	if hash != request.RequestHash {
		return contract.Publication{}, errors.New("field presentation command differs from canonical request")
	}
	return manager.Publish(ctx, request, func(store ports.MutationStore) error {
		t, err := store.Tenants().GetByID(ctx, request.TenantID)
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
		if table.TenantID != request.TenantID || table.Archived {
			return errors.New("active tenant-owned table required")
		}
		field, err := store.Fields().GetByID(ctx, input.FieldID)
		if err != nil {
			return err
		}
		if field.TenantID != request.TenantID || field.TableID != request.TableID || field.Archived {
			return errors.New("active field in publication table required")
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
		if err := recordSchemaChange(ctx, store.SchemaChanges(), s.idGenerator.New(), field.TenantID, "update_field", "field", field.ID, field.UpdatedAt, map[string]any{"table_id": field.TableID, "name": field.Name, "nullable": field.Nullable, "is_enum": field.IsEnum, "is_unique": field.IsUnique, "description": field.Description}); err != nil {
			return err
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, field.TenantID, schemaMigrationVersion("update_field", "field"), field.UpdatedAt)
	})
}
