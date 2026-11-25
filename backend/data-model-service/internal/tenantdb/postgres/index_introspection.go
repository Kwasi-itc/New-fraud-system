package postgres

import (
	"context"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

func (m SchemaManager) GetManagedIndexState(
	ctx context.Context,
	record tenant.Tenant,
	table datamodel.Table,
	job datamodel.IndexJob,
) (datamodel.ManagedIndexState, error) {
	indexName := managedIndexNameWithIncludes(table.Name, job.Columns, job.IncludeColumns, false)
	var valid bool
	var exists bool
	if m.pool != nil {
		if err := m.db.QueryRow(ctx, `
			SELECT
				EXISTS (
					SELECT 1
					FROM pg_class c
					JOIN pg_namespace n ON n.oid = c.relnamespace
					WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = 'i'
				) AS exists,
				EXISTS (
					SELECT 1
					FROM pg_class c
					JOIN pg_namespace n ON n.oid = c.relnamespace
					JOIN pg_index i ON i.indexrelid = c.oid
					WHERE n.nspname = $1 AND c.relname = $2 AND i.indisvalid AND i.indisready
				) AS valid
		`, record.SchemaName, indexName).Scan(&exists, &valid); err != nil {
			return datamodel.ManagedIndexState{}, fmt.Errorf("lookup managed index state: %w", err)
		}
		return datamodel.ManagedIndexState{
			Name:   indexName,
			Exists: exists,
			Valid:  valid,
		}, nil
	}

	if err := m.db.QueryRow(ctx, `
		SELECT to_regclass($1) IS NOT NULL
	`, fmt.Sprintf("%s.%s", record.SchemaName, indexName)).Scan(&exists); err != nil {
		return datamodel.ManagedIndexState{}, fmt.Errorf("lookup managed index state: %w", err)
	}
	return datamodel.ManagedIndexState{
		Name:   indexName,
		Exists: exists,
		Valid:  exists,
	}, nil
}
