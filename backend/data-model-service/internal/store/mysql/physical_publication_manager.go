package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/google/uuid"
)

// PhysicalPublicationAuthorizer binds the actual caller to the request's actor,
// tenant and table before database locks, on every invocation including replay.
type PhysicalPublicationAuthorizer func(context.Context, uuid.UUID, ports.PhysicalPublicationRequest) error

type PhysicalPublicationManager struct {
	metadata    TransactionManager
	publication *PublicationStore
	executor    *PhysicalExecutor
	table       uuid.UUID
	policies    []PhysicalAccessPolicy
	authorize   PhysicalPublicationAuthorizer
}

// NewPhysicalPublicationManager binds one authorized table snapshot and copies
// reviewed policies. Neither the snapshot nor these callbacks constitutes runtime
// wiring; concrete caller identity and owner business checks remain mandatory.
func NewPhysicalPublicationManager(metadata *TransactionManager, publication *PublicationStore, table PhysicalTable, policies []PhysicalAccessPolicy, authorize PhysicalPublicationAuthorizer) (*PhysicalPublicationManager, error) {
	if metadata == nil || metadata.db == nil || metadata.tenant == uuid.Nil || metadata.authorize == nil || publication == nil || publication.db != metadata.db || authorize == nil || table.TableID == uuid.Nil {
		return nil, errors.New("same-pool scoped metadata/publication, registered table and authorizer required")
	}
	executor, err := NewPhysicalExecutor(metadata.db, metadata.database, metadata.tenant)
	if err != nil {
		return nil, err
	}
	if publication.schema != executor.schema {
		return nil, errors.New("publication database differs from metadata")
	}
	if err := validatePhysicalPolicies(metadata.database, table.Name, policies); err != nil {
		return nil, err
	}
	copyPolicies := make([]PhysicalAccessPolicy, len(policies))
	for i, policy := range policies {
		copyPolicies[i] = policy
		objects := make([]installation.ObjectGrant, len(policy.Account.Objects))
		for j, object := range policy.Account.Objects {
			objects[j] = object
			objects[j].Privileges = append([]string(nil), object.Privileges...)
			objects[j].Columns = make(map[string][]string, len(object.Columns))
			for privilege, columns := range object.Columns {
				objects[j].Columns[privilege] = append([]string(nil), columns...)
			}
		}
		copyPolicies[i].Account.Objects = objects
	}
	return &PhysicalPublicationManager{*metadata, publication, executor, table.TableID, copyPolicies, authorize}, nil
}

func (m *PhysicalPublicationManager) Publish(ctx context.Context, request ports.PhysicalPublicationRequest, mutate func(ports.MutationStore) error) (contract.Publication, error) {
	return m.publish(ctx, request, mutate, nil)
}
func (m *PhysicalPublicationManager) publish(ctx context.Context, request ports.PhysicalPublicationRequest, mutate func(ports.MutationStore) error, retained *metadataResult) (contract.Publication, error) {
	if m == nil || m.executor == nil || mutate == nil || request.TableID != m.table {
		return contract.Publication{}, errors.New("initialized table-bound publisher and owner mutation required")
	}
	r := PhysicalPublicationRequest{TableID: request.TableID, Operation: PublicationOperation{ID: request.OperationID, RequestHash: request.RequestHash, Condition: request.Condition}, ActorKind: request.ActorKind, ActorID: request.ActorID}
	var receipt func(*sql.Tx, bool) error
	if retained != nil {
		receipt = func(tx *sql.Tx, replay bool) error {
			results := ownerResults{m.publication.schema, m.metadata.tenant, m.metadata.maxBytes}
			if replay {
				var err error
				retained.raw, err = results.read(ctx, tx, request.TableID, request.OperationID, retained.kind)
				if err != nil {
					return err
				}
			}
			if err := retained.decode(m.metadata.tenant, request.TableID); err != nil {
				return err
			}
			if !replay {
				return results.write(ctx, tx, request.TableID, request.OperationID, retained.kind, retained.raw)
			}
			return nil
		}
	}
	return m.executor.publishVerifiedOperation(ctx, m.publication, r, m.policies,
		func(ctx context.Context, tenant uuid.UUID, _ PhysicalPublicationRequest) error {
			return m.authorize(ctx, tenant, request)
		},
		func(tx *sql.Tx) error {
			// Recheck local permission in the exact owner transaction. The outer
			// authorizer handles historical replay before locks, even when no
			// owner transaction/callback is needed.
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
			if err := mutate(metadataMutationStore{repositories, tx, m.metadata.tenant, m.metadata.queue}); err != nil {
				return err
			}
			if retained != nil {
				return retained.capture(ctx, repositories, request.TableID)
			}
			return nil
		}, receipt)
}

var _ ports.PhysicalPublicationManager = (*PhysicalPublicationManager)(nil)

// WithCreationReadiness binds the initial-creation dependency contract while
// preserving the driver's owner port, authorization and atomic metadata effects.
func (m *PhysicalPublicationManager) WithCreationReadiness(shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy) (*PhysicalPublicationManager, error) {
	if m == nil || m.executor == nil {
		return nil, errors.New("initialized publication manager required")
	}
	e, err := m.executor.WithCreationReadiness(shared, installer)
	if err != nil {
		return nil, err
	}
	copy := *m
	copy.executor = e
	return &copy, nil
}
