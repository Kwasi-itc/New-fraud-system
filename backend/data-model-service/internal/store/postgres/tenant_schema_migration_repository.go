package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type TenantSchemaMigrationRepository struct {
	db executor
}

func NewTenantSchemaMigrationRepository(db executor) TenantSchemaMigrationRepository {
	return TenantSchemaMigrationRepository{db: db}
}

func (r TenantSchemaMigrationRepository) Create(ctx context.Context, migration datamodel.TenantSchemaMigration) error {
	query := `
		INSERT INTO core.tenant_schema_migrations (id, tenant_id, version, applied_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.Exec(ctx, query, migration.ID, migration.TenantID, migration.Version, migration.AppliedAt)
	if err != nil {
		return fmt.Errorf("insert tenant schema migration: %w", err)
	}
	return nil
}

func (r TenantSchemaMigrationRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.TenantSchemaMigration, error) {
	query := `
		SELECT id, tenant_id, version, applied_at
		FROM core.tenant_schema_migrations
		WHERE tenant_id = $1
		ORDER BY applied_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant schema migrations: %w", err)
	}
	defer rows.Close()

	var migrations []datamodel.TenantSchemaMigration
	for rows.Next() {
		var migration datamodel.TenantSchemaMigration
		if err := rows.Scan(&migration.ID, &migration.TenantID, &migration.Version, &migration.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan tenant schema migration: %w", err)
		}
		migrations = append(migrations, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant schema migrations: %w", err)
	}
	return migrations, nil
}
