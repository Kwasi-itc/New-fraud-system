package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (p *LocalCreation) DisplaySettings(ctx context.Context, id uuid.UUID) (display.Settings, error) {
	if err := localAuthority(ctx, nil); err != nil {
		return display.Settings{}, err
	}
	var tenant mysqlstore.UUID
	if err := p.Target.QueryRowContext(ctx, "SELECT tenant_id FROM fraud.data_model_tables WHERE id=?", mysqlstore.UUID(id)).Scan(&tenant); err != nil {
		return display.Settings{}, err
	}
	s, err := store.NewDisplayStore(p.Target, "fraud", uuid.UUID(tenant), localMaxRows, localMaxBytes)
	if err != nil {
		return display.Settings{}, err
	}
	return s.Get(ctx, id)
}
func (p *LocalCreation) SaveDisplaySettings(ctx context.Context, id uuid.UUID, input display.SaveRequest, op uuid.UUID) (display.Settings, error) {
	var empty display.Settings
	if err := localAuthority(ctx, nil); err != nil {
		return empty, err
	}
	var tenant, version mysqlstore.UUID
	err := p.Target.QueryRowContext(ctx, "SELECT t.tenant_id,p.published_version FROM fraud.data_model_tables t JOIN fraud.data_model_physical_tables p ON p.tenant_id=t.tenant_id AND p.table_id=t.id WHERE t.id=?", mysqlstore.UUID(id)).Scan(&tenant, &version)
	if err != nil {
		return empty, err
	}
	var prior mysqlstore.UUID
	err = p.Target.QueryRowContext(ctx, "SELECT expected_version FROM fraud.data_model_publication_operations WHERE tenant_id=? AND table_id=? AND operation_id=?", tenant, mysqlstore.UUID(id), mysqlstore.UUID(op)).Scan(&prior)
	if err == nil {
		version = prior
	} else if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	queue, err := localIndexQueue(p.Target)
	if err != nil {
		return empty, err
	}
	metadata, err := store.NewTransactionManager(p.Target, "fraud", uuid.UUID(tenant), localMaxRows, localMaxBytes, localTenantAuthority, queue)
	if err != nil {
		return empty, err
	}
	settings, err := store.NewDisplayStore(p.Target, "fraud", uuid.UUID(tenant), localMaxRows, localMaxBytes)
	if err != nil {
		return empty, err
	}
	publication, err := store.NewPublicationStore(p.Target, "fraud", localMaxRows, localMaxRows, localMaxBytes)
	if err != nil {
		return empty, err
	}
	actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	manager, err := store.NewDisplayPublicationManager(metadata, settings, publication, func(ctx context.Context, t, table, a uuid.UUID) error {
		if t != uuid.UUID(tenant) || table != id || a != actor {
			return store.ErrPhysicalConflict
		}
		return localAuthority(ctx, nil)
	})
	if err != nil {
		return empty, err
	}
	owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, localIDs{}, localClock{})
	return owner.PublishDisplaySettingsResult(ctx, display.PublicationRequest{TenantID: uuid.UUID(tenant), TableID: id, ActorID: actor, OperationID: op, Condition: store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(version)}, Settings: input}, manager)
}
func (p *LocalCreation) RecordDisplays(ctx context.Context, tenant uuid.UUID, objects []display.Object) (display.Batch, error) {
	if err := localAuthority(ctx, nil); err != nil {
		return display.Batch{}, err
	}
	s, err := store.NewDisplayStore(p.Target, "fraud", tenant, localMaxRows, localMaxBytes)
	if err != nil {
		return display.Batch{}, err
	}
	return s.ReadRecords(ctx, objects)
}
