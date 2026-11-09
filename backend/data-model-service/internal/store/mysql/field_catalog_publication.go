package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PublishCatalogField publishes enum flags and presentation changes without
// pretending that unchanged physical DDL is a migration. It excludes physical
// writers while replacing their contract version, preserving the physical epoch.
// Nullability, uniqueness, identity and lifecycle changes are rejected by the
// scoped repository. Historical replay never updates today's physical gate.
func (m *MetadataPublicationManager) PublishCatalogField(ctx context.Context, r ports.MetadataPublicationRequest, field uuid.UUID, effects func(ports.MutationStore) error) (datamodel.Field, error) {
	if m == nil || field == uuid.Nil || r.TenantID != m.metadata.tenant || effects == nil || r.ActorID == uuid.Nil || r.Condition.Mode != contract.PublicationExactVersion {
		return datamodel.Field{}, errors.New("scoped field catalog publisher required")
	}
	if err := m.authorize(ctx, r); err != nil {
		return datamodel.Field{}, err
	}
	op := PublicationOperation{ID: r.OperationID, RequestHash: r.RequestHash, Condition: r.Condition}
	retained := metadataResult{kind: fieldResultKind, id: field}
	err := mysqlstore.Mutation(ctx, m.metadata.db, func(tx *sql.Tx) error {
		changed := false
		result, err := m.publication.MutateAndPublishOperationTx(ctx, tx, r.TenantID, r.TableID, op, func(tx *sql.Tx) error {
			if err := m.metadata.authorize(ctx, tx, r.TenantID); err != nil {
				return err
			}
			var version mysqlstore.UUID
			if err := tx.QueryRowContext(ctx, "SELECT published_version FROM "+m.publication.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? AND write_state='open' AND active_operation IS NULL FOR UPDATE", mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.TableID)).Scan(&version); err != nil {
				return err
			}
			if uuid.UUID(version) != r.Condition.Version {
				return ErrPhysicalConflict
			}
			repos, err := NewMetadataRepositories(tx, m.metadata.database, r.TenantID, m.metadata.maxRows, m.metadata.maxBytes)
			if err != nil {
				return err
			}
			scoped := catalogMutationStore{metadataMutationStore{repos, tx, r.TenantID, m.metadata.queue}, r.TableID, field}
			if err := effects(scoped); err != nil {
				return err
			}
			changed = true
			return retained.capture(ctx, repos, r.TableID)
		})
		if err != nil {
			return err
		}
		receipts := ownerResults{m.publication.schema, r.TenantID, m.metadata.maxBytes}
		if changed {
			if result.Version != r.Condition.Version {
				updated, err := tx.ExecContext(ctx, "UPDATE "+m.publication.schema+".data_model_physical_tables SET published_version=? WHERE tenant_id=? AND table_id=? AND published_version=? AND write_state='open' AND active_operation IS NULL", mysqlstore.UUID(result.Version), mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.TableID), mysqlstore.UUID(r.Condition.Version))
				if err := physicalPublicationChanged(updated, err); err != nil {
					return err
				}
			}
			if err := receipts.write(ctx, tx, r.TableID, r.OperationID, fieldResultKind, retained.raw); err != nil {
				return err
			}
		} else {
			retained.raw, err = receipts.read(ctx, tx, r.TableID, r.OperationID, fieldResultKind)
			if err != nil {
				return err
			}
		}
		return retained.decode(r.TenantID, r.TableID)
	})
	if err != nil {
		return datamodel.Field{}, err
	}
	return retained.field, nil
}

type catalogMutationStore struct {
	metadataMutationStore
	table, field uuid.UUID
}

func (s catalogMutationStore) Fields() ports.FieldRepository {
	return catalogFields{s.repositories.Fields, s.table, s.field}
}

type catalogFields struct {
	FieldRepository
	table, field uuid.UUID
}

func (r catalogFields) Update(ctx context.Context, v datamodel.Field) error {
	if v.ID != r.field || v.TableID != r.table || v.TenantID != r.tenant {
		return ErrPhysicalConflict
	}
	prior, err := r.GetByID(ctx, v.ID)
	if err != nil {
		return err
	}
	prior.Alias, prior.Description, prior.IsEnum, prior.UpdatedAt = v.Alias, v.Description, v.IsEnum, v.UpdatedAt
	if prior != v {
		return ErrPhysicalConflict
	}
	return r.FieldRepository.Update(ctx, v)
}
