package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type FieldEnumValueRepository struct{ metadataRepository }

func (r FieldEnumValueRepository) validateValue(v datamodel.FieldEnumValue) error {
	if err := r.validate(v.TenantID, v.ID, v.Value, v.Label); err != nil {
		return err
	}
	if v.FieldID == uuid.Nil || v.SortOrder < -2147483648 || v.SortOrder > 2147483647 {
		return errors.New("enum field and signed INT sort order required")
	}
	return nil
}

func (r FieldEnumValueRepository) Create(ctx context.Context, v datamodel.FieldEnumValue) error {
	if err := r.validateValue(v); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_field_enum_values(id,tenant_id,field_id,value,label,sort_order,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.FieldID), v.Value, v.Label, v.SortOrder, mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt})
	return err
}

func (r FieldEnumValueRepository) selectSQL() string {
	return "SELECT id,tenant_id,field_id," + r.projection("value", "label") + ",sort_order,created_at,updated_at FROM " + r.schema + ".data_model_field_enum_values WHERE tenant_id=?"
}

func scanEnum(row metadataScanner) (datamodel.FieldEnumValue, int64, error) {
	var v datamodel.FieldEnumValue
	var size int64
	var created, updated mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.FieldID), &size, &v.Value, &v.Label, &v.SortOrder, &created, &updated)
	v.CreatedAt, v.UpdatedAt = created.Time, updated.Time
	return v, size, err
}

func (r FieldEnumValueRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.FieldEnumValue, error) {
	return metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" AND id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)), scanEnum)
}

func (r FieldEnumValueRepository) ListByField(ctx context.Context, id uuid.UUID) ([]datamodel.FieldEnumValue, error) {
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" AND field_id=? ORDER BY sort_order,created_at,id", []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(id)}, scanEnum)
}

func (r FieldEnumValueRepository) Update(ctx context.Context, v datamodel.FieldEnumValue) error {
	if err := r.validateValue(v); err != nil {
		return err
	}
	if err := r.lock(ctx, "data_model_field_enum_values", v.ID, "field_id", v.FieldID); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_field_enum_values SET value=?,label=?,sort_order=?,updated_at=? WHERE tenant_id=? AND field_id=? AND id=?", v.Value, v.Label, v.SortOrder, mysqlstore.DateTime{Time: v.UpdatedAt}, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.FieldID), mysqlstore.UUID(v.ID))
	return err
}

func (r FieldEnumValueRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.delete(ctx, "data_model_field_enum_values", id)
}
