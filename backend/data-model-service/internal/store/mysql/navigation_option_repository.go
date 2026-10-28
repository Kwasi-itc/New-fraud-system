package mysql

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type NavigationOptionRepository struct{ metadataRepository }

func (r NavigationOptionRepository) Create(ctx context.Context, v datamodel.NavigationOption) error {
	if err := r.validate(v.TenantID, v.ID); err != nil {
		return err
	}
	if v.SourceTableID == uuid.Nil || v.SourceFieldID == uuid.Nil || v.TargetTableID == uuid.Nil || v.FilterFieldID == uuid.Nil || v.OrderingFieldID == uuid.Nil {
		return errors.New("navigation endpoints required")
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_navigation_options(id,tenant_id,source_table_id,source_field_id,target_table_id,filter_field_id,ordering_field_id,created_at) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.SourceTableID), mysqlstore.UUID(v.SourceFieldID), mysqlstore.UUID(v.TargetTableID), mysqlstore.UUID(v.FilterFieldID), mysqlstore.UUID(v.OrderingFieldID), mysqlstore.DateTime{Time: v.CreatedAt})
	return err
}

// Fetch all resolved names in one statement. Every join includes tenant and
// table ownership; no scanning of unrelated models or N+1 relationship reads.
func (r NavigationOptionRepository) selectSQL() string {
	return "SELECT n.id,n.tenant_id,n.source_table_id,n.source_field_id,n.target_table_id,n.filter_field_id,n.ordering_field_id," + r.projection("st.name", "sf.name", "tt.name", "ff.name", "ofld.name") + ",n.created_at FROM " + r.schema + ".data_model_navigation_options n" +
		" JOIN " + r.schema + ".data_model_tables st ON st.tenant_id=n.tenant_id AND st.id=n.source_table_id" +
		" JOIN " + r.schema + ".data_model_fields sf ON sf.tenant_id=n.tenant_id AND sf.table_id=n.source_table_id AND sf.id=n.source_field_id" +
		" JOIN " + r.schema + ".data_model_tables tt ON tt.tenant_id=n.tenant_id AND tt.id=n.target_table_id" +
		" JOIN " + r.schema + ".data_model_fields ff ON ff.tenant_id=n.tenant_id AND ff.table_id=n.target_table_id AND ff.id=n.filter_field_id" +
		" JOIN " + r.schema + ".data_model_fields ofld ON ofld.tenant_id=n.tenant_id AND ofld.table_id=n.target_table_id AND ofld.id=n.ordering_field_id WHERE n.tenant_id=?"
}

func scanNavigation(row metadataScanner) (datamodel.NavigationOption, int64, error) {
	var v datamodel.NavigationOption
	var size int64
	var created mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.SourceTableID), (*mysqlstore.UUID)(&v.SourceFieldID), (*mysqlstore.UUID)(&v.TargetTableID), (*mysqlstore.UUID)(&v.FilterFieldID), (*mysqlstore.UUID)(&v.OrderingFieldID), &size, &v.SourceTableName, &v.SourceFieldName, &v.TargetTableName, &v.FilterFieldName, &v.OrderingFieldName, &created)
	v.CreatedAt = created.Time
	return v, size, err
}

func (r NavigationOptionRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.NavigationOption, error) {
	return metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" AND n.id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), scanNavigation)
}

func (r NavigationOptionRepository) ListByTenant(ctx context.Context, id uuid.UUID) ([]datamodel.NavigationOption, error) {
	if err := r.validate(id, id); err != nil {
		return nil, err
	}
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" ORDER BY n.created_at,n.id", []any{mysqlstore.UUID(r.tenant)}, scanNavigation)
}

func (r NavigationOptionRepository) ListBySourceTable(ctx context.Context, id uuid.UUID) ([]datamodel.NavigationOption, error) {
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" AND n.source_table_id=? ORDER BY n.created_at,n.id", []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)}, scanNavigation)
}

func (r NavigationOptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.delete(ctx, "data_model_navigation_options", id)
}
