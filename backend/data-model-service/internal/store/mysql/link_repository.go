package mysql

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type LinkRepository struct{ metadataRepository }

func (r LinkRepository) Create(ctx context.Context, v datamodel.Link) error {
	if err := r.validate(v.TenantID, v.ID, v.Name); err != nil {
		return err
	}
	if v.ParentTable == uuid.Nil || v.ParentField == uuid.Nil || v.ChildTable == uuid.Nil || v.ChildField == uuid.Nil {
		return errors.New("link endpoints required")
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_links(id,tenant_id,name,parent_table_id,parent_field_id,child_table_id,child_field_id,created_at) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), v.Name, mysqlstore.UUID(v.ParentTable), mysqlstore.UUID(v.ParentField), mysqlstore.UUID(v.ChildTable), mysqlstore.UUID(v.ChildField), mysqlstore.DateTime{Time: v.CreatedAt})
	return err
}

func (r LinkRepository) selectSQL() string {
	return "SELECT id,tenant_id," + r.projection("name") + ",parent_table_id,parent_field_id,child_table_id,child_field_id,created_at FROM " + r.schema + ".data_model_links WHERE tenant_id=?"
}

func scanLink(row metadataScanner) (datamodel.Link, int64, error) {
	var v datamodel.Link
	var size int64
	var created mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), &size, &v.Name, (*mysqlstore.UUID)(&v.ParentTable), (*mysqlstore.UUID)(&v.ParentField), (*mysqlstore.UUID)(&v.ChildTable), (*mysqlstore.UUID)(&v.ChildField), &created)
	v.CreatedAt = created.Time
	return v, size, err
}

func (r LinkRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Link, error) {
	return metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), scanLink)
}

func (r LinkRepository) ListByTenant(ctx context.Context, id uuid.UUID) ([]datamodel.Link, error) {
	if err := r.validate(id, id); err != nil {
		return nil, err
	}
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" ORDER BY created_at DESC,id DESC", []any{mysqlstore.UUID(r.tenant)}, scanLink)
}

// Delete only removes metadata. Owner services must first clean dependent
// display definitions and pivot paths in the same transaction.
func (r LinkRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.delete(ctx, "data_model_links", id)
}
