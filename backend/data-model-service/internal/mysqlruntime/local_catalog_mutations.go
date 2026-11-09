package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (p *LocalCreation) DeleteField(ctx context.Context, id uuid.UUID, dry bool, op uuid.UUID) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()
	if err := localAuthority(ctx, nil); err != nil {
		return report, err
	}
	var tenant, table mysqlstore.UUID
	if err := p.Target.QueryRowContext(ctx, "SELECT tenant_id,table_id FROM fraud.data_model_fields WHERE id=?", mysqlstore.UUID(id)).Scan(&tenant, &table); err != nil {
		return report, err
	}
	err := mysqlstore.Mutation(ctx, p.Target, func(tx *sql.Tx) error {
		var locked mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR UPDATE", tenant, table).Scan(&locked); err != nil {
			return err
		}
		var err error
		report, err = store.CheckFieldDeletion(ctx, tx, "fraud", uuid.UUID(tenant), uuid.UUID(table), id)
		return err
	})
	if err != nil || dry {
		return report, err
	}
	_, err = p.mutatePhysicalField(ctx, service.UpdateFieldInput{FieldID: id}, op, true)
	report.Performed = err == nil
	return report, err
}

func (p *LocalCreation) MutateEnum(ctx context.Context, input service.UpdateFieldEnumValueInput, remove bool, op uuid.UUID) (datamodel.FieldEnumValue, error) {
	var zero datamodel.FieldEnumValue
	if err := localAuthority(ctx, nil); err != nil {
		return zero, err
	}
	var tenant, table, version mysqlstore.UUID
	err := p.Target.QueryRowContext(ctx, "SELECT f.tenant_id,f.table_id,p.published_version FROM fraud.data_model_field_enum_values e JOIN fraud.data_model_fields f ON f.tenant_id=e.tenant_id AND f.id=e.field_id JOIN fraud.data_model_physical_tables p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id WHERE e.id=?", mysqlstore.UUID(input.EnumValueID)).Scan(&tenant, &table, &version)
	if err != nil {
		return zero, err
	}
	var original mysqlstore.UUID
	err = p.Target.QueryRowContext(ctx, "SELECT expected_version FROM fraud.data_model_publication_operations WHERE tenant_id=? AND table_id=? AND operation_id=?", tenant, table, mysqlstore.UUID(op)).Scan(&original)
	if err == nil {
		version = original
	} else if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	condition := store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(version)}
	var command any = input
	if remove {
		command = service.DeleteEnumValueInput{EnumValueID: input.EnumValueID}
	}
	hash, err := service.PublicationRequestHash(service.PublicationRequestContext{TenantID: uuid.UUID(tenant), TableID: uuid.UUID(table), ActorID: actor, Condition: condition}, command)
	if err != nil {
		return zero, err
	}
	metadata, err := store.NewTransactionManager(p.Target, "fraud", uuid.UUID(tenant), localMaxRows, localMaxBytes, localTenantAuthority, nil)
	if err != nil {
		return zero, err
	}
	publication, err := store.NewPublicationStore(p.Target, "fraud", localMaxRows, localMaxRows, localMaxBytes)
	if err != nil {
		return zero, err
	}
	request := ports.MetadataPublicationRequest{TenantID: uuid.UUID(tenant), TableID: uuid.UUID(table), ActorID: actor, OperationID: op, RequestHash: hash, Condition: condition}
	manager, err := store.NewMetadataPublicationManager(metadata, publication, func(ctx context.Context, r ports.MetadataPublicationRequest) error {
		if r != request {
			return store.ErrPhysicalConflict
		}
		return localAuthority(ctx, nil)
	})
	if err != nil {
		return zero, err
	}
	owner := service.NewFieldEnumValueService(nil, nil, nil, nil, localIDs{}, localClock{})
	return manager.PublishEnum(ctx, request, input.EnumValueID, func(tx ports.MutationStore) (datamodel.FieldEnumValue, error) {
		return owner.ApplyCatalogMutation(ctx, tx, input, remove, uuid.UUID(tenant), uuid.UUID(table))
	})
}

func (p *LocalCreation) CreateEnum(ctx context.Context, input service.CreateFieldEnumValueInput, op uuid.UUID) (datamodel.FieldEnumValue, error) {
	var zero datamodel.FieldEnumValue
	if err := localAuthority(ctx, nil); err != nil {
		return zero, err
	}
	var tenant, table, version mysqlstore.UUID
	err := p.Target.QueryRowContext(ctx, "SELECT f.tenant_id,f.table_id,p.published_version FROM fraud.data_model_fields f JOIN fraud.data_model_physical_tables p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id WHERE f.id=?", mysqlstore.UUID(input.FieldID)).Scan(&tenant, &table, &version)
	if err != nil {
		return zero, err
	}
	var original mysqlstore.UUID
	err = p.Target.QueryRowContext(ctx, "SELECT expected_version FROM fraud.data_model_publication_operations WHERE tenant_id=? AND table_id=? AND operation_id=?", tenant, table, mysqlstore.UUID(op)).Scan(&original)
	if err == nil {
		version = original
	} else if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	condition := store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(version)}
	hash, err := service.PublicationRequestHash(service.PublicationRequestContext{TenantID: uuid.UUID(tenant), TableID: uuid.UUID(table), ActorID: actor, Condition: condition}, input)
	if err != nil {
		return zero, err
	}
	metadata, err := store.NewTransactionManager(p.Target, "fraud", uuid.UUID(tenant), localMaxRows, localMaxBytes, localTenantAuthority, nil)
	if err != nil {
		return zero, err
	}
	publication, err := store.NewPublicationStore(p.Target, "fraud", localMaxRows, localMaxRows, localMaxBytes)
	if err != nil {
		return zero, err
	}
	request := ports.MetadataPublicationRequest{TenantID: uuid.UUID(tenant), TableID: uuid.UUID(table), ActorID: actor, OperationID: op, RequestHash: hash, Condition: condition}
	manager, err := store.NewMetadataPublicationManager(metadata, publication, func(ctx context.Context, r ports.MetadataPublicationRequest) error {
		if r != request {
			return store.ErrPhysicalConflict
		}
		return localAuthority(ctx, nil)
	})
	if err != nil {
		return zero, err
	}
	owner := service.NewFieldEnumValueService(nil, nil, nil, nil, localIDs{}, localClock{})
	id := uuid.NewSHA1(input.FieldID, []byte("create-enum/v1/"+op.String()))
	return manager.PublishEnumCreation(ctx, request, id, input.FieldID, func(tx ports.MutationStore) (datamodel.FieldEnumValue, error) {
		return owner.ApplyCatalogCreation(ctx, tx, input, id, uuid.UUID(tenant), uuid.UUID(table))
	})
}

func (p *LocalCreation) DeleteTable(ctx context.Context, id uuid.UUID, dry bool, op uuid.UUID) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()
	if err := localAuthority(ctx, nil); err != nil {
		return report, err
	}
	var tenant mysqlstore.UUID
	if err := p.Target.QueryRowContext(ctx, "SELECT tenant_id FROM fraud.data_model_tables WHERE id=?", mysqlstore.UUID(id)).Scan(&tenant); err != nil {
		return report, err
	}
	manager, err := store.NewTableRetirement(p.Target, "fraud", uuid.UUID(tenant), localTenantAuthority)
	if err != nil {
		return report, err
	}
	return manager.Delete(ctx, id, op, dry)
}
