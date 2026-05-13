package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type SchemaChangeRepository struct {
	db executor
}

func NewSchemaChangeRepository(db executor) SchemaChangeRepository {
	return SchemaChangeRepository{db: db}
}

func (r SchemaChangeRepository) Create(ctx context.Context, change datamodel.SchemaChange) error {
	query := `
		INSERT INTO core.schema_change_log (id, tenant_id, operation, resource_type, resource_id, status, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.Exec(ctx, query,
		change.ID, change.TenantID, change.Operation, change.ResourceType, change.ResourceID, change.Status, change.Details, change.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert schema change: %w", err)
	}
	return nil
}

func (r SchemaChangeRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.SchemaChange, error) {
	query := `
		SELECT id, tenant_id, operation, resource_type, resource_id, status, details, created_at
		FROM core.schema_change_log
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list schema changes: %w", err)
	}
	defer rows.Close()

	var changes []datamodel.SchemaChange
	for rows.Next() {
		var change datamodel.SchemaChange
		if err := rows.Scan(
			&change.ID, &change.TenantID, &change.Operation, &change.ResourceType, &change.ResourceID, &change.Status, &change.Details, &change.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan schema change: %w", err)
		}
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema changes: %w", err)
	}
	return changes, nil
}
