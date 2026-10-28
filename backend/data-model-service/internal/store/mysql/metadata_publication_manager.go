package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type MetadataPublicationAuthorizer func(context.Context, ports.MetadataPublicationRequest) error

type MetadataPublicationManager struct {
	metadata    TransactionManager
	publication PublicationStore
	authorize   MetadataPublicationAuthorizer
}

func NewMetadataPublicationManager(metadata *TransactionManager, publication *PublicationStore, authorize MetadataPublicationAuthorizer) (*MetadataPublicationManager, error) {
	if metadata == nil || publication == nil || authorize == nil || metadata.authorize == nil || metadata.db == nil || metadata.tenant == uuid.Nil || metadata.db != publication.db {
		return nil, errors.New("same-pool scoped metadata/publication and authorizers required")
	}
	schema, err := mysqlstore.Identifier(metadata.database)
	if err != nil {
		return nil, err
	}
	if schema != publication.schema {
		return nil, errors.New("publication database differs from metadata")
	}
	return &MetadataPublicationManager{*metadata, *publication, authorize}, nil
}

func (m *MetadataPublicationManager) Publish(ctx context.Context, request ports.MetadataPublicationRequest, effects func(ports.MutationStore) error) (contract.Publication, error) {
	return m.publish(ctx, request, effects, nil)
}

func (m *MetadataPublicationManager) publish(ctx context.Context, request ports.MetadataPublicationRequest, effects func(ports.MutationStore) error, retained *metadataResult) (contract.Publication, error) {
	if m == nil || m.metadata.db == nil || effects == nil || request.TenantID != m.metadata.tenant || request.TableID == uuid.Nil || request.ActorID == uuid.Nil || request.Condition.Mode != contract.PublicationExactVersion {
		return contract.Publication{}, errors.New("scoped presentation command and exact prior publication required")
	}
	op := PublicationOperation{ID: request.OperationID, RequestHash: request.RequestHash, Condition: request.Condition}
	if err := op.validate(); err != nil {
		return contract.Publication{}, err
	}
	if err := m.authorize(ctx, request); err != nil {
		return contract.Publication{}, err
	}
	var result contract.Publication
	err := mysqlstore.Mutation(ctx, m.metadata.db, func(tx *sql.Tx) error {
		var err error
		result, err = m.publication.MutateAndPublishOperationTx(ctx, tx, request.TenantID, request.TableID, op, func(tx *sql.Tx) error {
			if err := m.metadata.authorize(ctx, tx, request.TenantID); err != nil {
				return err
			}
			registry, err := NewPhysicalRegistry(tx, m.metadata.database, request.TenantID, m.metadata.maxRows)
			if err != nil {
				return err
			}
			if _, err = registry.LockPublished(ctx, request.TableID, request.Condition.Version); err != nil {
				return err
			}
			repos, err := NewMetadataRepositories(tx, m.metadata.database, request.TenantID, m.metadata.maxRows, m.metadata.maxBytes)
			if err != nil {
				return err
			}
			before, err := repos.Tables.GetByID(ctx, request.TableID)
			if err != nil {
				return err
			}
			if before.Archived {
				return ErrPhysicalConflict
			}
			if err := effects(presentationMutationStore{metadataMutationStore{repos, tx, request.TenantID, m.metadata.queue}, request.TableID}); err != nil {
				return err
			}
			after, err := repos.Tables.GetByID(ctx, request.TableID)
			if err != nil {
				return err
			}
			// Caption and archive have lifecycle effects absent from ingestion
			// JSON. An unchanged semantic token cannot authorize either bypass.
			if after.Archived || after.CaptionField != before.CaptionField {
				return ErrPhysicalConflict
			}
			if retained != nil {
				return retained.capture(ctx, repos, request.TableID)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if result.Version != request.Condition.Version {
			return ErrPhysicalConflict
		}
		if retained != nil {
			receipts := ownerResults{m.publication.schema, request.TenantID, m.metadata.maxBytes}
			if retained.raw == nil {
				retained.raw, err = receipts.read(ctx, tx, request.TableID, request.OperationID, retained.kind)
				if err != nil {
					return err
				}
			} else {
				if err = receipts.write(ctx, tx, request.TableID, request.OperationID, retained.kind, retained.raw); err != nil {
					return err
				}
			}
			if err = retained.decode(request.TenantID, request.TableID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return contract.Publication{}, err
	}
	return result, nil
}

var _ ports.MetadataPublicationManager = (*MetadataPublicationManager)(nil)
