package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
)

type TableOptionsRepository struct {
	db executor
}

func NewTableOptionsRepository(db executor) TableOptionsRepository {
	return TableOptionsRepository{db: db}
}

func (r TableOptionsRepository) GetByTableID(ctx context.Context, tableID uuid.UUID) (*datamodel.TableOptions, error) {
	query := `
		SELECT id, tenant_id, table_id, displayed_fields, field_order, updated_at
		FROM core.table_options
		WHERE table_id = $1
	`
	var options datamodel.TableOptions
	err := r.db.QueryRow(ctx, query, tableID).Scan(
		&options.ID, &options.TenantID, &options.TableID, &options.DisplayedFields, &options.FieldOrder, &options.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get table options: %w", err)
	}
	return &options, nil
}

func (r TableOptionsRepository) Upsert(ctx context.Context, options datamodel.TableOptions) error {
	query := `
		INSERT INTO core.table_options (id, tenant_id, table_id, displayed_fields, field_order, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (table_id)
		DO UPDATE SET
			displayed_fields = EXCLUDED.displayed_fields,
			field_order = EXCLUDED.field_order,
			updated_at = EXCLUDED.updated_at
	`
	_, err := r.db.Exec(ctx, query,
		options.ID, options.TenantID, options.TableID, options.DisplayedFields, options.FieldOrder, options.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert table options: %w", err)
	}
	return nil
}
