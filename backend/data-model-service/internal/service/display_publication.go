package service

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

// PublishDisplaySettings joins the concrete caption-index/revision effects to
// authorized display publication. It returns the original publication receipt,
// not saved/current Settings, and does not change the existing HTTP response.
func (s TableService) PublishDisplaySettings(ctx context.Context, request display.PublicationRequest, manager display.PublicationManager) (contract.Publication, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil {
		return contract.Publication{}, errors.New("display publisher and owner dependencies required")
	}
	if _, err := display.SaveRequestHash(request.TenantID, request.TableID, request.ActorID, request.Condition, request.Settings); err != nil {
		return contract.Publication{}, err
	}
	return manager.Publish(ctx, request, s.displayPublicationEffects(ctx, request))
}

// PublishDisplaySettingsResult returns the original saved Settings on replay.
func (s TableService) PublishDisplaySettingsResult(ctx context.Context, request display.PublicationRequest, manager display.SettingsPublicationManager) (display.Settings, error) {
	if manager == nil || s.idGenerator == nil || s.clock == nil {
		return display.Settings{}, errors.New("display publisher and owner dependencies required")
	}
	if _, err := display.SaveRequestHash(request.TenantID, request.TableID, request.ActorID, request.Condition, request.Settings); err != nil {
		return display.Settings{}, err
	}
	return manager.PublishSettings(ctx, request, s.displayPublicationEffects(ctx, request))
}

func (s TableService) displayPublicationEffects(ctx context.Context, request display.PublicationRequest) func(ports.MutationStore) error {
	return func(store ports.MutationStore) error {
		t, err := store.Tenants().GetByID(ctx, request.TenantID)
		if err != nil {
			return err
		}
		if t.Status != tenant.StatusActive {
			return errors.New("active tenant required for display publication")
		}
		table, err := store.Tables().GetByID(ctx, request.TableID)
		if err != nil {
			return err
		}
		if table.TenantID != request.TenantID || table.Archived || table.CaptionField != request.Settings.Caption {
			return errors.New("display owner table or validated caption mismatch")
		}
		now := s.clock.Now()
		if table.CaptionField != "" {
			if _, _, err := ensureManagedIndexJobTx(ctx, store, s.idGenerator, table.TenantID, table, datamodel.IndexJobTypeSearch, []string{table.CaptionField}, "update_table_caption_field", now); err != nil {
				return err
			}
		}
		return recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, table.TenantID, schemaMigrationVersion("update_display_settings", "table"), now)
	}
}
