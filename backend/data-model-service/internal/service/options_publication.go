package service

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// ReplaceTableOptionsInput contains only caller-owned values. Identity, tenant
// and timestamps are resolved by the authorized owner, never supplied as output.
type ReplaceTableOptionsInput struct {
	TableID         uuid.UUID
	DisplayedFields []uuid.UUID
	FieldOrder      []uuid.UUID
}

// PublishReplacement preserves the legacy options contract. Display JSON and
// relation dependencies remain owned by the separate display coordinator.
// The manager must authorize every replay and retain the original response.
func (s OptionsService) PublishReplacement(ctx context.Context, input ReplaceTableOptionsInput, request ports.MetadataPublicationRequest, manager ports.OptionsPublicationManager) (datamodel.TableOptions, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil || input.TableID != request.TableID {
		return datamodel.TableOptions{}, errors.New("options publisher, owner dependencies and matching table required")
	}
	hash, err := PublicationRequestHash(PublicationRequestContext{TenantID: request.TenantID, TableID: request.TableID, ActorID: request.ActorID, Condition: request.Condition}, input)
	if err != nil {
		return datamodel.TableOptions{}, err
	}
	if hash != request.RequestHash {
		return datamodel.TableOptions{}, errors.New("options command differs from canonical request")
	}
	if len(input.DisplayedFields) > 12 {
		return datamodel.TableOptions{}, errors.New("at most 12 displayed fields are allowed")
	}
	for _, ids := range [][]uuid.UUID{input.DisplayedFields, input.FieldOrder} {
		seen := make(map[uuid.UUID]bool, len(ids))
		for _, id := range ids {
			if id == uuid.Nil || seen[id] {
				return datamodel.TableOptions{}, errors.New("option fields must be nonzero and distinct within each list")
			}
			seen[id] = true
		}
	}
	return manager.PublishOptions(ctx, request, func(store ports.MutationStore) error {
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
		fields, err := store.Fields().ListByTable(ctx, table.ID)
		if err != nil {
			return err
		}
		active := make(map[uuid.UUID]bool, len(fields))
		for _, f := range fields {
			if f.TenantID != request.TenantID || f.TableID != table.ID {
				return errors.New("foreign options field")
			}
			active[f.ID] = !f.Archived
		}
		for _, ids := range [][]uuid.UUID{input.DisplayedFields, input.FieldOrder} {
			for _, id := range ids {
				if !active[id] {
					return errors.New("option field is not active in table")
				}
			}
		}
		prior, err := store.TableOptions().GetByTableID(ctx, table.ID)
		if err != nil {
			return err
		}
		id := uuid.Nil
		if prior != nil {
			id = prior.ID
		} else {
			id = s.idGenerator.New()
		}
		if id == uuid.Nil {
			return errors.New("nonzero options identity required")
		}
		now := s.clock.Now()
		value := datamodel.TableOptions{ID: id, TenantID: request.TenantID, TableID: table.ID, DisplayedFields: input.DisplayedFields, FieldOrder: input.FieldOrder, UpdatedAt: now}
		if err := store.TableOptions().Upsert(ctx, value); err != nil {
			return err
		}
		if err := recordSchemaChange(ctx, store.SchemaChanges(), s.idGenerator.New(), request.TenantID, "upsert_table_options", "table_options", table.ID, now, map[string]any{"displayed_fields": input.DisplayedFields, "field_order": input.FieldOrder}); err != nil {
			return err
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, request.TenantID, schemaMigrationVersion("upsert_table_options", "table_options"), now)
	})
}
