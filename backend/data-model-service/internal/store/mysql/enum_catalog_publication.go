package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// PublishEnum serializes with physical writers and publishes catalog, audit,
// version and historical response in one transaction, including deletion.
func (m *MetadataPublicationManager) PublishEnum(ctx context.Context, r ports.MetadataPublicationRequest, id uuid.UUID, effects func(ports.MutationStore) (datamodel.FieldEnumValue, error)) (datamodel.FieldEnumValue, error) {
	return m.publishEnum(ctx, r, id, uuid.Nil, effects)
}

// PublishEnumCreation binds a preallocated value identity to its scoped field.
func (m *MetadataPublicationManager) PublishEnumCreation(ctx context.Context, r ports.MetadataPublicationRequest, id, field uuid.UUID, effects func(ports.MutationStore) (datamodel.FieldEnumValue, error)) (datamodel.FieldEnumValue, error) {
	if field == uuid.Nil {
		return datamodel.FieldEnumValue{}, ErrPhysicalConflict
	}
	return m.publishEnum(ctx, r, id, field, effects)
}
func (m *MetadataPublicationManager) publishEnum(ctx context.Context, r ports.MetadataPublicationRequest, id, createField uuid.UUID, effects func(ports.MutationStore) (datamodel.FieldEnumValue, error)) (datamodel.FieldEnumValue, error) {
	var value datamodel.FieldEnumValue
	if m == nil || r.TenantID != m.metadata.tenant || r.ActorID == uuid.Nil || r.TableID == uuid.Nil || id == uuid.Nil || effects == nil || r.Condition.Mode != contract.PublicationExactVersion {
		return value, ErrPhysicalConflict
	}
	if err := m.authorize(ctx, r); err != nil {
		return value, err
	}
	op := PublicationOperation{ID: r.OperationID, RequestHash: r.RequestHash, Condition: r.Condition}
	const kind = "datamodel.enum/v1"
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
			fieldID := createField
			if fieldID == uuid.Nil {
				original, e := repos.Enums.GetByID(ctx, id)
				if e != nil {
					return e
				}
				fieldID = original.FieldID
			}
			field, err := repos.Fields.GetByID(ctx, fieldID)
			if err != nil {
				return err
			}
			if field.TableID != r.TableID {
				return ErrPhysicalConflict
			}
			value, err = effects(metadataMutationStore{repos, tx, r.TenantID, m.metadata.queue})
			if err != nil {
				return err
			}
			if value.ID != id || value.TenantID != r.TenantID || value.FieldID != field.ID {
				return ErrPhysicalConflict
			}
			changed = true
			return nil
		})
		if err != nil {
			return err
		}
		receipts := ownerResults{m.publication.schema, r.TenantID, m.metadata.maxBytes}
		if changed {
			if result.Version != r.Condition.Version {
				updated, e := tx.ExecContext(ctx, "UPDATE "+m.publication.schema+".data_model_physical_tables SET published_version=? WHERE tenant_id=? AND table_id=? AND published_version=? AND write_state='open' AND active_operation IS NULL", mysqlstore.UUID(result.Version), mysqlstore.UUID(r.TenantID), mysqlstore.UUID(r.TableID), mysqlstore.UUID(r.Condition.Version))
				if e = physicalPublicationChanged(updated, e); e != nil {
					return e
				}
			}
			raw, e := json.Marshal(value)
			if e != nil {
				return e
			}
			return receipts.write(ctx, tx, r.TableID, r.OperationID, kind, raw)
		}
		raw, e := receipts.read(ctx, tx, r.TableID, r.OperationID, kind)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &value); e != nil {
			return e
		}
		if value.ID != id || value.TenantID != r.TenantID || value.FieldID == uuid.Nil {
			return ErrPublicationOperationConflict
		}
		return nil
	})
	return value, err
}
