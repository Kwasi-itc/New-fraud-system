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
	indexName := managedIndexName(table.Name, job.Columns, false)
	var exists bool
	if err := m.db.QueryRow(ctx, `
		SELECT to_regclass($1) IS NOT NULL
	`, fmt.Sprintf("%s.%s", record.SchemaName, indexName)).Scan(&exists); err != nil {
		return datamodel.ManagedIndexState{}, fmt.Errorf("lookup managed index state: %w", err)
	}
	return datamodel.ManagedIndexState{
		Name:   indexName,
		Exists: exists,
	}, nil
}
