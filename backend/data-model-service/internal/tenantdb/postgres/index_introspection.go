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
	indexName := job.IndexName
	if indexName == "" {
		indexName = managedIndexName(table.Name, job.Columns, job.IsUnique)
	}
	var exists, valid, ready bool
	if err := m.db.QueryRow(ctx, `
		SELECT
			COUNT(*) > 0,
			COALESCE(BOOL_AND(i.indisvalid), FALSE),
			COALESCE(BOOL_AND(i.indisready), FALSE)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_index i ON i.indexrelid = c.oid
		WHERE n.nspname = $1 AND c.relname = $2
		`, record.SchemaName, indexName).Scan(&exists, &valid, &ready); err != nil {
		return datamodel.ManagedIndexState{}, fmt.Errorf("lookup managed index state: %w", err)
	}
	return datamodel.ManagedIndexState{
		Name:          indexName,
		Exists:        exists,
		Valid:         valid,
		Ready:         ready,
		ValidityKnown: true,
	}, nil
}

func (m SchemaManager) DropInvalidManagedIndex(
	ctx context.Context,
	record tenant.Tenant,
	table datamodel.Table,
	job datamodel.IndexJob,
) error {
	state, err := m.GetManagedIndexState(ctx, record, table, job)
	if err != nil {
		return err
	}
	if !state.Exists || (state.Valid && state.Ready) {
		return nil
	}
	query := fmt.Sprintf(
		"DROP INDEX CONCURRENTLY IF EXISTS %s",
		sanitizeIdentifier(record.SchemaName, state.Name),
	)
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("drop invalid managed index: %w", err)
	}
	return nil
}

func (m SchemaManager) HasNullValue(
	ctx context.Context,
	record tenant.Tenant,
	table datamodel.Table,
	fieldName string,
) (bool, error) {
	query := fmt.Sprintf(
		"SELECT EXISTS (SELECT 1 FROM %s WHERE %s IS NULL LIMIT 1)",
		sanitizeIdentifier(record.SchemaName, table.Name),
		sanitizeIdentifier(fieldName),
	)
	var exists bool
	if err := m.db.QueryRow(ctx, query).Scan(&exists); err != nil {
		return false, fmt.Errorf("check logical bucket timestamp nulls: %w", err)
	}
	return exists, nil
}
