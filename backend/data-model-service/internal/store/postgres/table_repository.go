package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type TableRepository struct {
	db executor
}

func NewTableRepository(db executor) TableRepository {
	return TableRepository{db: db}
}

func (r TableRepository) Create(ctx context.Context, table datamodel.Table) error {
	query := `
		INSERT INTO core.model_tables
			(id, tenant_id, name, description, alias, semantic_type, caption_field, storage_class,
			 event_time_field, event_schema_revision, event_schema_locked_at, storage_cutover_at, legacy_read_until, archived, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`
	_, err := r.db.Exec(ctx, query,
		table.ID, table.TenantID, table.Name, table.Description, table.Alias,
		table.SemanticType, table.CaptionField, table.StorageClass, table.EventTimeField,
		table.EventSchemaRevision, table.EventSchemaLockedAt, table.StorageCutoverAt, table.LegacyReadUntil,
		table.Archived, table.CreatedAt, table.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert table: %w", err)
	}
	return nil
}

func (r TableRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Table, error) {
	query := `
		SELECT id, tenant_id, name, description, alias, semantic_type, caption_field, storage_class,
		       event_time_field, event_schema_revision, event_schema_locked_at, storage_cutover_at, legacy_read_until, archived, created_at, updated_at
		FROM core.model_tables
		WHERE id = $1
	`
	var table datamodel.Table
	err := r.db.QueryRow(ctx, query, id).Scan(
		&table.ID, &table.TenantID, &table.Name, &table.Description, &table.Alias,
		&table.SemanticType, &table.CaptionField, &table.StorageClass, &table.EventTimeField,
		&table.EventSchemaRevision, &table.EventSchemaLockedAt, &table.StorageCutoverAt, &table.LegacyReadUntil,
		&table.Archived, &table.CreatedAt, &table.UpdatedAt,
	)
	if err != nil {
		return datamodel.Table{}, fmt.Errorf("get table by id: %w", err)
	}
	return table, nil
}

func (r TableRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Table, error) {
	query := `
		SELECT id, tenant_id, name, description, alias, semantic_type, caption_field, storage_class,
		       event_time_field, event_schema_revision, event_schema_locked_at, storage_cutover_at, legacy_read_until, archived, created_at, updated_at
		FROM core.model_tables
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tables by tenant: %w", err)
	}
	defer rows.Close()

	var tables []datamodel.Table
	for rows.Next() {
		var table datamodel.Table
		if err := rows.Scan(
			&table.ID, &table.TenantID, &table.Name, &table.Description, &table.Alias,
			&table.SemanticType, &table.CaptionField, &table.StorageClass, &table.EventTimeField,
			&table.EventSchemaRevision, &table.EventSchemaLockedAt, &table.StorageCutoverAt, &table.LegacyReadUntil,
			&table.Archived, &table.CreatedAt, &table.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}
	return tables, nil
}

func (r TableRepository) Update(ctx context.Context, table datamodel.Table) error {
	query := `
		UPDATE core.model_tables
		SET description = $2, alias = $3, semantic_type = $4, caption_field = $5,
		    storage_class = $6, event_time_field = $7, storage_cutover_at = $8,
		    legacy_read_until = $9, archived = $10, updated_at = $11
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query, table.ID, table.Description, table.Alias, table.SemanticType,
		table.CaptionField, table.StorageClass, table.EventTimeField, table.StorageCutoverAt,
		table.LegacyReadUntil, table.Archived, table.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update table: %w", err)
	}
	return nil
}

func (r TableRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM core.model_tables WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete table: %w", err)
	}
	return nil
}
