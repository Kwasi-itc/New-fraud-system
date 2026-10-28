package service

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

// PublishPresentationUpdate handles description/alias/semantic-type edits on an
// existing table. Caption changes require the multi-table display coordinator.
// It returns a replayable ingestion publication, not a historical Table result.
func (s TableService) PublishPresentationUpdate(ctx context.Context, input UpdateTableInput, request ports.MetadataPublicationRequest, manager ports.MetadataPublicationManager) (contract.Publication, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil || input.TableID != request.TableID || input.CaptionField != nil {
		return contract.Publication{}, errors.New("presentation publisher and owner dependencies required; caption uses display protocol")
	}
	hash, err := PublicationRequestHash(PublicationRequestContext{TenantID: request.TenantID, TableID: request.TableID, ActorID: request.ActorID, Condition: request.Condition}, input)
	if err != nil {
		return contract.Publication{}, err
	}
	if hash != request.RequestHash {
		return contract.Publication{}, errors.New("presentation command differs from canonical request")
	}
	if input.SemanticType != nil {
		if err := datamodel.ValidateSemanticType(*input.SemanticType); err != nil {
			return contract.Publication{}, err
		}
	}
	return manager.Publish(ctx, request, func(store ports.MutationStore) error {
		t, err := store.Tenants().GetByID(ctx, request.TenantID)
		if err != nil {
			return err
		}
		if t.Status != tenant.StatusActive {
			return errors.New("active tenant required")
		}
		table, err := store.Tables().GetByID(ctx, input.TableID)
		if err != nil {
			return err
		}
		if table.TenantID != request.TenantID || table.Archived {
			return errors.New("active tenant-owned table required")
		}
		if input.Description != nil {
			table.Description = *input.Description
		}
		if input.Alias != nil {
			table.Alias = *input.Alias
		}
		if input.SemanticType != nil {
			table.SemanticType = datamodel.NormalizeName(*input.SemanticType)
		}
		table.UpdatedAt = s.clock.Now()
		if err := store.Tables().Update(ctx, table); err != nil {
			return err
		}
		details := map[string]any{"description": table.Description, "alias": table.Alias, "semantic_type": table.SemanticType, "caption_field": table.CaptionField}
		if table.CaptionField != "" {
			job, requested, err := ensureManagedIndexJobTx(ctx, store, s.idGenerator, table.TenantID, table, datamodel.IndexJobTypeSearch, []string{table.CaptionField}, "update_table_caption_field", table.UpdatedAt)
			if err != nil {
				return err
			}
			if requested {
				details["search_index_job_id"] = job.ID
				details["search_index_requested"] = true
			}
		}
		if err := recordSchemaChange(ctx, store.SchemaChanges(), s.idGenerator.New(), table.TenantID, "update_table", "table", table.ID, table.UpdatedAt, details); err != nil {
			return err
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, table.TenantID, schemaMigrationVersion("update_table", "table"), table.UpdatedAt)
	})
}
