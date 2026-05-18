package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type FieldEnumValueRepository struct {
	db executor
}

func NewFieldEnumValueRepository(db executor) FieldEnumValueRepository {
	return FieldEnumValueRepository{db: db}
}

func (r FieldEnumValueRepository) Create(ctx context.Context, value datamodel.FieldEnumValue) error {
	query := `
		INSERT INTO core.field_enum_values
			(id, tenant_id, field_id, value, label, sort_order, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
	`
	if _, err := r.db.Exec(ctx, query,
		value.ID, value.TenantID, value.FieldID, value.Value, value.Label, value.SortOrder, value.CreatedAt, value.UpdatedAt,
	); err != nil {
		return fmt.Errorf("insert field enum value: %w", err)
	}
	return nil
}

func (r FieldEnumValueRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.FieldEnumValue, error) {
	query := `
		SELECT id, tenant_id, field_id, value, label, sort_order, created_at, updated_at
		FROM core.field_enum_values
		WHERE id = $1
	`
	var value datamodel.FieldEnumValue
	if err := r.db.QueryRow(ctx, query, id).Scan(
		&value.ID, &value.TenantID, &value.FieldID, &value.Value, &value.Label, &value.SortOrder, &value.CreatedAt, &value.UpdatedAt,
	); err != nil {
		return datamodel.FieldEnumValue{}, fmt.Errorf("get field enum value by id: %w", err)
	}
	return value, nil
}

func (r FieldEnumValueRepository) ListByField(ctx context.Context, fieldID uuid.UUID) ([]datamodel.FieldEnumValue, error) {
	query := `
		SELECT id, tenant_id, field_id, value, label, sort_order, created_at, updated_at
		FROM core.field_enum_values
		WHERE field_id = $1
		ORDER BY sort_order ASC, created_at ASC
	`
	rows, err := r.db.Query(ctx, query, fieldID)
	if err != nil {
		return nil, fmt.Errorf("list field enum values by field: %w", err)
	}
	defer rows.Close()

	values := []datamodel.FieldEnumValue{}
	for rows.Next() {
		var value datamodel.FieldEnumValue
		if err := rows.Scan(
			&value.ID, &value.TenantID, &value.FieldID, &value.Value, &value.Label, &value.SortOrder, &value.CreatedAt, &value.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan field enum value: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate field enum values: %w", err)
	}
	return values, nil
}

func (r FieldEnumValueRepository) Update(ctx context.Context, value datamodel.FieldEnumValue) error {
	query := `
		UPDATE core.field_enum_values
		SET value = $2, label = $3, sort_order = $4, updated_at = $5
		WHERE id = $1
	`
	if _, err := r.db.Exec(ctx, query, value.ID, value.Value, value.Label, value.SortOrder, value.UpdatedAt); err != nil {
		return fmt.Errorf("update field enum value: %w", err)
	}
	return nil
}

func (r FieldEnumValueRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM core.field_enum_values WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete field enum value: %w", err)
	}
	return nil
}
