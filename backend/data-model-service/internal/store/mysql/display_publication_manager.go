package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type DisplayPublicationAuthorizer func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error

// DisplayPublicationManager binds a tenant and same-pool transaction resources.
// Construction does not activate a backend or confer identity authorization.
type DisplayPublicationManager struct {
	metadata    TransactionManager
	display     DisplayStore
	publication PublicationStore
	authorize   DisplayPublicationAuthorizer
}

func NewDisplayPublicationManager(metadata *TransactionManager, settings *DisplayStore, publication *PublicationStore, authorize DisplayPublicationAuthorizer) (*DisplayPublicationManager, error) {
	if metadata == nil || settings == nil || publication == nil || authorize == nil || metadata.authorize == nil || metadata.db == nil || metadata.tenant == uuid.Nil || metadata.db != settings.db || metadata.db != publication.db || metadata.database != settings.database || settings.schema != publication.schema || metadata.tenant != settings.tenant {
		return nil, errors.New("same-pool tenant-bound display/publication/metadata and authorizers required")
	}
	return &DisplayPublicationManager{*metadata, *settings, *publication, authorize}, nil
}

func (m *DisplayPublicationManager) Publish(ctx context.Context, request display.PublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
	if m == nil || m.metadata.db == nil || request.TenantID != m.metadata.tenant || effects == nil {
		return contract.Publication{}, errors.New("initialized scoped display publisher and owner effects required")
	}
	return m.display.SavePublished(ctx, request.TableID, request.ActorID, request.OperationID, request.Condition, request.Settings, &m.publication, m.authorize, m.ownerEffects(effects))
}

func (m *DisplayPublicationManager) PublishSettings(ctx context.Context, request display.PublicationRequest, effects func(ports.MutationStore) error) (display.Settings, error) {
	if m == nil || m.metadata.db == nil || request.TenantID != m.metadata.tenant || effects == nil {
		return display.Settings{}, errors.New("initialized scoped display publisher and owner effects required")
	}
	return m.display.SavePublishedSettings(ctx, request.TableID, request.ActorID, request.OperationID, request.Condition, request.Settings, &m.publication, m.authorize, m.ownerEffects(effects))
}

var _ display.PublicationManager = (*DisplayPublicationManager)(nil)

var _ display.SettingsPublicationManager = (*DisplayPublicationManager)(nil)

func (m *DisplayPublicationManager) ownerEffects(effects func(ports.MutationStore) error) func(context.Context, *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error {
		if err := m.metadata.authorize(ctx, tx, m.metadata.tenant); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		repositories, err := NewMetadataRepositories(tx, m.metadata.database, m.metadata.tenant, m.metadata.maxRows, m.metadata.maxBytes)
		if err != nil {
			return err
		}
		return effects(metadataMutationStore{repositories, tx, m.metadata.tenant, m.metadata.queue})
	}
}
