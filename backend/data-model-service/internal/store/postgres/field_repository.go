package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type FieldRepository struct {
	db executor
}

func NewFieldRepository(db executor) FieldRepository {
	return FieldRepository{db: db}
}

func (r FieldRepository) Create(ctx context.Context, field datamodel.Field) error {
	query := `
		INSERT INTO core.model_fields
			(id, tenant_id, table_id, name, description, data_type, nullable, is_enum, is_unique, archived, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := r.db.Exec(ctx, query,
		field.ID, field.TenantID, field.TableID, field.Name, field.Description, field.DataType,
		field.Nullable, field.IsEnum, field.IsUnique, field.Archived, field.CreatedAt, field.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert field: %w", err)
	}
	return nil
}

func (r FieldRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Field, error) {
	query := `
		SELECT id, tenant_id, table_id, name, description, data_type, nullable, is_enum, is_unique, archived, created_at, updated_at
		FROM core.model_fields
		WHERE id = $1
	`
	var field datamodel.Field
	err := r.db.QueryRow(ctx, query, id).Scan(
		&field.ID, &field.TenantID, &field.TableID, &field.Name, &field.Description, &field.DataType,
		&field.Nullable, &field.IsEnum, &field.IsUnique, &field.Archived, &field.CreatedAt, &field.UpdatedAt,
	)
	if err != nil {
		return datamodel.Field{}, fmt.Errorf("get field by id: %w", err)
	}
	return field, nil
}

func (r FieldRepository) ListByTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.Field, error) {
	query := `
		SELECT id, tenant_id, table_id, name, description, data_type, nullable, is_enum, is_unique, archived, created_at, updated_at
		FROM core.model_fields
		WHERE table_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.Query(ctx, query, tableID)
	if err != nil {
		return nil, fmt.Errorf("list fields by table: %w", err)
	}
	defer rows.Close()

	var fields []datamodel.Field
	for rows.Next() {
		var field datamodel.Field
		if err := rows.Scan(
			&field.ID, &field.TenantID, &field.TableID, &field.Name, &field.Description, &field.DataType,
			&field.Nullable, &field.IsEnum, &field.IsUnique, &field.Archived, &field.CreatedAt, &field.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan field: %w", err)
		}
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fields: %w", err)
	}
	return fields, nil
}

func (r FieldRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM core.model_fields WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete field: %w", err)
	}
	return nil
}

func (r FieldRepository) Update(ctx context.Context, field datamodel.Field) error {
	query := `
		UPDATE core.model_fields
		SET description = $2, nullable = $3, is_enum = $4, is_unique = $5, archived = $6, updated_at = $7
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query, field.ID, field.Description, field.Nullable, field.IsEnum, field.IsUnique, field.Archived, field.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update field: %w", err)
	}
	return nil
}
