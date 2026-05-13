package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type PivotRepository struct {
	db executor
}

func NewPivotRepository(db executor) PivotRepository {
	return PivotRepository{db: db}
}

func (r PivotRepository) Create(ctx context.Context, pivot datamodel.Pivot) error {
	query := `
		INSERT INTO core.model_pivots
			(id, tenant_id, base_table_id, field_id, path_link_ids, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(ctx, query, pivot.ID, pivot.TenantID, pivot.BaseTableID, pivot.FieldID, pivot.PathLinkIDs, pivot.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert pivot: %w", err)
	}
	return nil
}

func (r PivotRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Pivot, error) {
	query := `
		SELECT id, tenant_id, base_table_id, field_id, path_link_ids, created_at
		FROM core.model_pivots
		WHERE id = $1
	`
	var pivot datamodel.Pivot
	err := r.db.QueryRow(ctx, query, id).Scan(
		&pivot.ID, &pivot.TenantID, &pivot.BaseTableID, &pivot.FieldID, &pivot.PathLinkIDs, &pivot.CreatedAt,
	)
	if err != nil {
		return datamodel.Pivot{}, fmt.Errorf("get pivot by id: %w", err)
	}
	return pivot, nil
}

func (r PivotRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Pivot, error) {
	query := `
		SELECT id, tenant_id, base_table_id, field_id, path_link_ids, created_at
		FROM core.model_pivots
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list pivots: %w", err)
	}
	defer rows.Close()

	var pivots []datamodel.Pivot
	for rows.Next() {
		var pivot datamodel.Pivot
		if err := rows.Scan(
			&pivot.ID, &pivot.TenantID, &pivot.BaseTableID, &pivot.FieldID, &pivot.PathLinkIDs, &pivot.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pivot: %w", err)
		}
		pivots = append(pivots, pivot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pivots: %w", err)
	}
	return pivots, nil
}

func (r PivotRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM core.model_pivots WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete pivot: %w", err)
	}
	return nil
}
