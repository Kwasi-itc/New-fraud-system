package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Presentation workflows have no HTTP dependency. Owner publication commits
// metadata, audit, revisions and the historical response atomically.
func (p *LocalCreation) UpdateTable(ctx context.Context, input service.UpdateTableInput, op uuid.UUID) (datamodel.Table, error) {
	value, err := p.updatePresentation(ctx, input.TableID, op, input, false)
	if err != nil {
		return datamodel.Table{}, err
	}
	return value.(datamodel.Table), nil
}
func (p *LocalCreation) UpdateField(ctx context.Context, input service.UpdateFieldInput, op uuid.UUID) (datamodel.Field, error) {
	if input.Nullable != nil || input.IsUnique != nil || input.IsEnum != nil {
		return p.updateFieldConstraints(ctx, input, op)
	}
	value, err := p.updatePresentation(ctx, input.FieldID, op, input, true)
	if err != nil {
		return datamodel.Field{}, err
	}
	return value.(datamodel.Field), nil
}
func (p *LocalCreation) updatePresentation(ctx context.Context, id, op uuid.UUID, input any, field bool) (any, error) {
	if err := localAuthority(ctx, nil); err != nil {
		return nil, err
	}
	var tenantID, tableID, version mysqlstore.UUID
	query := "SELECT t.tenant_id,t.id,p.published_version FROM fraud.data_model_tables t JOIN fraud.data_model_physical_tables p ON p.tenant_id=t.tenant_id AND p.table_id=t.id WHERE t.id=?"
	if field {
		query = "SELECT f.tenant_id,f.table_id,p.published_version FROM fraud.data_model_fields f JOIN fraud.data_model_physical_tables p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id WHERE f.id=?"
	}
	err := p.Target.QueryRowContext(ctx, query, mysqlstore.UUID(id)).Scan(&tenantID, &tableID, &version)
	if err != nil {
		return nil, err
	}
	// A historical retry must use its original precondition even when a
	// later physical field change advanced the current contract version.
	var original mysqlstore.UUID
	err = p.Target.QueryRowContext(ctx, "SELECT expected_version FROM fraud.data_model_publication_operations WHERE tenant_id=? AND table_id=? AND operation_id=?", tenantID, tableID, mysqlstore.UUID(op)).Scan(&original)
	if err == nil {
		version = original
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	queue, err := localIndexQueue(p.Target)
	if err != nil {
		return nil, err
	}
	metadata, err := store.NewTransactionManager(p.Target, "fraud", uuid.UUID(tenantID), 1000, 4<<20, localTenantAuthority, queue)
	if err != nil {
		return nil, err
	}
	publication, err := store.NewPublicationStore(p.Target, "fraud", 1000, 1000, 4<<20)
	if err != nil {
		return nil, err
	}
	actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	condition := contract.PublicationCondition{Mode: contract.PublicationExactVersion, Version: uuid.UUID(version)}
	hash, err := service.PublicationRequestHash(service.PublicationRequestContext{TenantID: uuid.UUID(tenantID), TableID: uuid.UUID(tableID), ActorID: actor, Condition: condition}, input)
	if err != nil {
		return nil, err
	}
	request := ports.MetadataPublicationRequest{TenantID: uuid.UUID(tenantID), TableID: uuid.UUID(tableID), ActorID: actor, OperationID: op, RequestHash: hash, Condition: condition}
	if !field {
		command := input.(service.UpdateTableInput)
		if command.CaptionField != nil {
			settings, err := store.NewDisplayStore(p.Target, "fraud", uuid.UUID(tenantID), localMaxRows, localMaxBytes)
			if err != nil {
				return nil, err
			}
			coordinator, err := store.NewDisplayPublicationManager(metadata, settings, publication, func(ctx context.Context, tenant, table, actorID uuid.UUID) error {
				if tenant != request.TenantID || table != request.TableID || actorID != request.ActorID {
					return store.ErrPhysicalConflict
				}
				return localAuthority(ctx, nil)
			})
			if err != nil {
				return nil, err
			}
			owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, localIDs{}, localClock{})
			return coordinator.PublishCaptionTable(ctx, request, datamodel.NormalizeName(*command.CaptionField), func(tx ports.MutationStore) error {
				return owner.ApplyCaptionPresentation(ctx, tx, command, request)
			})
		}
	}
	manager, err := store.NewMetadataPublicationManager(metadata, publication, func(ctx context.Context, actual ports.MetadataPublicationRequest) error {
		if actual != request {
			return errors.New("presentation scope mismatch")
		}
		return localAuthority(ctx, nil)
	})
	if err != nil {
		return nil, err
	}
	if field {
		owner := service.NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, localIDs{}, localClock{})
		value, err := owner.PublishPresentationResult(ctx, input.(service.UpdateFieldInput), request, manager)
		if err != nil {
			return nil, err
		}
		return value, nil
	} else {
		owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, localIDs{}, localClock{})
		value, err := owner.PublishPresentationResult(ctx, input.(service.UpdateTableInput), request, manager)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
}
