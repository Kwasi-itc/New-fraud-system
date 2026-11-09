package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

// PublishCaptionTable preserves unrelated display settings and retains the table
// PATCH response with its original command hash. A concurrent settings edit
// conflicts rather than being overwritten. Historical retries bypass current
// display topology but still require authorization and the original hash.
func (m *DisplayPublicationManager) PublishCaptionTable(ctx context.Context, request ports.MetadataPublicationRequest, caption string, effects func(ports.MutationStore) error) (datamodel.Table, error) {
	if m == nil || request.TenantID != m.metadata.tenant || request.OperationID == uuid.Nil || effects == nil {
		return datamodel.Table{}, errors.New("scoped caption publisher required")
	}
	if err := m.authorize(ctx, request.TenantID, request.TableID, request.ActorID); err != nil {
		return datamodel.Table{}, err
	}
	state := &displayPublication{store: &m.publication, operation: PublicationOperation{ID: request.OperationID, RequestHash: request.RequestHash, Condition: request.Condition}, tableResult: &metadataResult{kind: tableResultKind, id: request.TableID}}
	_, err := m.publication.RecoverPublication(ctx, request.TenantID, request.TableID, state.operation)
	if err == nil {
		err = m.display.recoverCaptionResult(ctx, m.metadata.db, request.TableID, state)
		return state.tableResult.table, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return datamodel.Table{}, err
	}
	current, err := m.display.Get(ctx, request.TableID)
	if err != nil {
		return datamodel.Table{}, err
	}
	in := display.SaveRequest{Version: &current.Version, Caption: caption, Displayed: current.Displayed, Order: current.Order, Display: current.Display, DisplayColumns: current.DisplayColumns}
	if _, err := m.display.save(ctx, request.TableID, in, m.ownerEffects(effects), state); err != nil {
		return datamodel.Table{}, err
	}
	return state.tableResult.table, nil
}

func (s *DisplayStore) recoverCaptionResult(ctx context.Context, q ownerResultReader, table uuid.UUID, state *displayPublication) error {
	raw, err := (ownerResults{s.schema, s.tenant, s.maxBytes}).read(ctx, q, table, state.operation.ID, tableResultKind)
	if err != nil {
		return err
	}
	state.tableResult.raw = raw
	return state.tableResult.decode(s.tenant, table)
}
