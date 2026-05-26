package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type NavigationOptionRepository struct {
	db executor
}

func NewNavigationOptionRepository(db executor) NavigationOptionRepository {
	return NavigationOptionRepository{db: db}
}

func (r NavigationOptionRepository) Create(ctx context.Context, option datamodel.NavigationOption) error {
	query := `
		INSERT INTO core.navigation_options (
			id, tenant_id, source_table_id, source_field_id, target_table_id,
			filter_field_id, ordering_field_id, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	if _, err := r.db.Exec(ctx, query,
		option.ID, option.TenantID, option.SourceTableID, option.SourceFieldID,
		option.TargetTableID, option.FilterFieldID, option.OrderingFieldID, option.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert navigation option: %w", err)
	}
	return nil
}

func (r NavigationOptionRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.NavigationOption, error) {
	query := `
		SELECT no.id, no.tenant_id, no.source_table_id, no.source_field_id, no.target_table_id,
			no.filter_field_id, no.ordering_field_id,
			st.name, sf.name, tt.name, ff.name, ofld.name,
			no.created_at
		FROM core.navigation_options no
		JOIN core.model_tables st ON st.id = no.source_table_id
		JOIN core.model_fields sf ON sf.id = no.source_field_id
		JOIN core.model_tables tt ON tt.id = no.target_table_id
		JOIN core.model_fields ff ON ff.id = no.filter_field_id
		JOIN core.model_fields ofld ON ofld.id = no.ordering_field_id
		WHERE no.id = $1
	`
	return scanNavigationOption(r.db.QueryRow(ctx, query, id))
}

func (r NavigationOptionRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.NavigationOption, error) {
	query := `
		SELECT no.id, no.tenant_id, no.source_table_id, no.source_field_id, no.target_table_id,
			no.filter_field_id, no.ordering_field_id,
			st.name, sf.name, tt.name, ff.name, ofld.name,
			no.created_at
		FROM core.navigation_options no
		JOIN core.model_tables st ON st.id = no.source_table_id
		JOIN core.model_fields sf ON sf.id = no.source_field_id
		JOIN core.model_tables tt ON tt.id = no.target_table_id
		JOIN core.model_fields ff ON ff.id = no.filter_field_id
		JOIN core.model_fields ofld ON ofld.id = no.ordering_field_id
		WHERE no.tenant_id = $1
		ORDER BY no.created_at ASC, no.id ASC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list navigation options: %w", err)
	}
	defer rows.Close()
	return collectNavigationOptions(rows)
}

func (r NavigationOptionRepository) ListBySourceTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.NavigationOption, error) {
	query := `
		SELECT no.id, no.tenant_id, no.source_table_id, no.source_field_id, no.target_table_id,
			no.filter_field_id, no.ordering_field_id,
			st.name, sf.name, tt.name, ff.name, ofld.name,
			no.created_at
		FROM core.navigation_options no
		JOIN core.model_tables st ON st.id = no.source_table_id
		JOIN core.model_fields sf ON sf.id = no.source_field_id
		JOIN core.model_tables tt ON tt.id = no.target_table_id
		JOIN core.model_fields ff ON ff.id = no.filter_field_id
		JOIN core.model_fields ofld ON ofld.id = no.ordering_field_id
		WHERE no.source_table_id = $1
		ORDER BY no.created_at ASC, no.id ASC
	`
	rows, err := r.db.Query(ctx, query, tableID)
	if err != nil {
		return nil, fmt.Errorf("list navigation options by source table: %w", err)
	}
	defer rows.Close()
	return collectNavigationOptions(rows)
}

func (r NavigationOptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM core.navigation_options WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete navigation option: %w", err)
	}
	return nil
}

type navigationOptionScanner interface {
	Scan(dest ...any) error
}

func scanNavigationOption(scanner navigationOptionScanner) (datamodel.NavigationOption, error) {
	var option datamodel.NavigationOption
	if err := scanner.Scan(
		&option.ID,
		&option.TenantID,
		&option.SourceTableID,
		&option.SourceFieldID,
		&option.TargetTableID,
		&option.FilterFieldID,
		&option.OrderingFieldID,
		&option.SourceTableName,
		&option.SourceFieldName,
		&option.TargetTableName,
		&option.FilterFieldName,
		&option.OrderingFieldName,
		&option.CreatedAt,
	); err != nil {
		return datamodel.NavigationOption{}, err
	}
	return option, nil
}

func collectNavigationOptions(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]datamodel.NavigationOption, error) {
	var options []datamodel.NavigationOption
	for rows.Next() {
		var option datamodel.NavigationOption
		if err := rows.Scan(
			&option.ID,
			&option.TenantID,
			&option.SourceTableID,
			&option.SourceFieldID,
			&option.TargetTableID,
			&option.FilterFieldID,
			&option.OrderingFieldID,
			&option.SourceTableName,
			&option.SourceFieldName,
			&option.TargetTableName,
			&option.FilterFieldName,
			&option.OrderingFieldName,
			&option.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan navigation option: %w", err)
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate navigation options: %w", err)
	}
	return options, nil
}
