package mysqlruntime

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/google/uuid"
)

type catalogFieldPublisher struct {
	manager *store.MetadataPublicationManager
	request ports.MetadataPublicationRequest
}

func (p catalogFieldPublisher) PublishField(ctx context.Context, r ports.PhysicalPublicationRequest, id uuid.UUID, effects func(ports.MutationStore) error) (datamodel.Field, error) {
	if r.OperationID != p.request.OperationID || r.RequestHash != p.request.RequestHash || r.TableID != p.request.TableID || r.Condition != p.request.Condition || r.ActorID != p.request.ActorID.String() || r.ActorKind != "api" {
		return datamodel.Field{}, store.ErrPhysicalConflict
	}
	return p.manager.PublishCatalogField(ctx, p.request, id, effects)
}
func (p *LocalCreation) publishFieldCatalog(ctx context.Context, tenant, table, op uuid.UUID, condition store.PublicationCondition, input service.UpdateFieldInput) (datamodel.Field, error) {
	actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	hash, err := service.PublicationRequestHash(service.PublicationRequestContext{TenantID: tenant, TableID: table, ActorID: actor, Condition: condition}, input)
	if err != nil {
		return datamodel.Field{}, err
	}
	metadata, err := store.NewTransactionManager(p.Target, "fraud", tenant, localMaxRows, localMaxBytes, localTenantAuthority, nil)
	if err != nil {
		return datamodel.Field{}, err
	}
	publication, err := store.NewPublicationStore(p.Target, "fraud", localMaxRows, localMaxRows, localMaxBytes)
	if err != nil {
		return datamodel.Field{}, err
	}
	request := ports.MetadataPublicationRequest{TenantID: tenant, TableID: table, ActorID: actor, OperationID: op, RequestHash: hash, Condition: condition}
	manager, err := store.NewMetadataPublicationManager(metadata, publication, func(ctx context.Context, r ports.MetadataPublicationRequest) error {
		if r != request {
			return store.ErrPhysicalConflict
		}
		return localAuthority(ctx, nil)
	})
	if err != nil {
		return datamodel.Field{}, err
	}
	owner := service.NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, localIDs{}, localClock{})
	physical := ports.PhysicalPublicationRequest{TableID: table, OperationID: op, RequestHash: hash, Condition: condition, ActorKind: "api", ActorID: actor.String()}
	return owner.PublishPreparedConstraintsResult(ctx, tenant, input, physical, catalogFieldPublisher{manager, request})
}
