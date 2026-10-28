package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type FieldRepository struct{ metadataRepository }

func (r FieldRepository) Create(ctx context.Context, v datamodel.Field) error {
	if err := r.validate(v.TenantID, v.ID, v.Name, v.Description, v.Alias, string(v.DataType)); err != nil {
		return err
	}
	if v.TableID == uuid.Nil {
		return errors.New("field table required")
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_fields(id,tenant_id,table_id,name,description,alias,data_type,nullable,is_enum,is_unique,archived,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.TableID), v.Name, v.Description, v.Alias, v.DataType, v.Nullable, v.IsEnum, v.IsUnique, v.Archived, mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt})
	return err
}

func (r FieldRepository) selectSQL() string {
	return "SELECT id,tenant_id,table_id," + r.projection("name", "description", "alias", "data_type") + ",nullable,is_enum,is_unique,archived,created_at,updated_at FROM " + r.schema + ".data_model_fields WHERE tenant_id=?"
}

func scanField(row metadataScanner) (datamodel.Field, int64, error) {
	var v datamodel.Field
	var size int64
	var created, updated mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.TableID), &size, &v.Name, &v.Description, &v.Alias, &v.DataType, &v.Nullable, &v.IsEnum, &v.IsUnique, &v.Archived, &created, &updated)
	v.CreatedAt, v.UpdatedAt = created.Time, updated.Time
	return v, size, err
}

func (r FieldRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Field, error) {
	return metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), scanField)
}

func (r FieldRepository) ListByTable(ctx context.Context, id uuid.UUID) ([]datamodel.Field, error) {
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" AND table_id=? ORDER BY created_at,id", []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)}, scanField)
}

func (r FieldRepository) Update(ctx context.Context, v datamodel.Field) error {
	if err := r.validate(v.TenantID, v.ID, v.Description, v.Alias); err != nil {
		return err
	}
	if err := r.lock(ctx, "data_model_fields", v.ID, "table_id", v.TableID); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_fields SET description=?,alias=?,nullable=?,is_enum=?,is_unique=?,archived=?,updated_at=? WHERE tenant_id=? AND table_id=? AND id=?", v.Description, v.Alias, v.Nullable, v.IsEnum, v.IsUnique, v.Archived, mysqlstore.DateTime{Time: v.UpdatedAt}, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID), mysqlstore.UUID(v.ID))
	return err
}

func (r FieldRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.delete(ctx, "data_model_fields", id)
}
