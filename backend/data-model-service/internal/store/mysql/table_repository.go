package mysql

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type TableRepository struct{ metadataRepository }

func (r TableRepository) Create(ctx context.Context, v datamodel.Table) error {
	if err := r.validate(v.TenantID, v.ID, v.Name, v.Description, v.Alias, v.SemanticType, v.CaptionField); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_tables(id,tenant_id,name,description,alias,semantic_type,caption_field,archived,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), v.Name, v.Description, v.Alias, v.SemanticType, v.CaptionField, v.Archived, mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt})
	return err
}

func (r TableRepository) selectSQL() string {
	return "SELECT id,tenant_id," + r.projection("name", "description", "alias", "semantic_type", "caption_field") + ",archived,created_at,updated_at FROM " + r.schema + ".data_model_tables WHERE tenant_id=?"
}

func scanTable(row metadataScanner) (datamodel.Table, int64, error) {
	var v datamodel.Table
	var size int64
	var created, updated mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), &size, &v.Name, &v.Description, &v.Alias, &v.SemanticType, &v.CaptionField, &v.Archived, &created, &updated)
	v.CreatedAt, v.UpdatedAt = created.Time, updated.Time
	return v, size, err
}

func (r TableRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Table, error) {
	return metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), scanTable)
}

func (r TableRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Table, error) {
	if err := r.validate(tenantID, tenantID); err != nil {
		return nil, err
	}
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" ORDER BY created_at DESC,id DESC", []any{mysqlstore.UUID(r.tenant)}, scanTable)
}

func (r TableRepository) Update(ctx context.Context, v datamodel.Table) error {
	if err := r.validate(v.TenantID, v.ID, v.Description, v.Alias, v.SemanticType, v.CaptionField); err != nil {
		return err
	}
	if err := r.lock(ctx, "data_model_tables", v.ID, "", uuid.Nil); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_tables SET description=?,alias=?,semantic_type=?,caption_field=?,archived=?,updated_at=? WHERE tenant_id=? AND id=?", v.Description, v.Alias, v.SemanticType, v.CaptionField, v.Archived, mysqlstore.DateTime{Time: v.UpdatedAt}, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.ID))
	return err
}

func (r TableRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.delete(ctx, "data_model_tables", id)
}
