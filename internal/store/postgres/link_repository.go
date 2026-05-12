package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
)

type LinkRepository struct {
	db executor
}

func NewLinkRepository(db executor) LinkRepository {
	return LinkRepository{db: db}
}

func (r LinkRepository) Create(ctx context.Context, link datamodel.Link) error {
	query := `
		INSERT INTO core.model_links
			(id, tenant_id, name, parent_table_id, parent_field_id, child_table_id, child_field_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.Exec(ctx, query,
		link.ID, link.TenantID, link.Name, link.ParentTable, link.ParentField, link.ChildTable, link.ChildField, link.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert link: %w", err)
	}
	return nil
}

func (r LinkRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Link, error) {
	query := `
		SELECT id, tenant_id, name, parent_table_id, parent_field_id, child_table_id, child_field_id, created_at
		FROM core.model_links
		WHERE id = $1
	`
	var link datamodel.Link
	err := r.db.QueryRow(ctx, query, id).Scan(
		&link.ID, &link.TenantID, &link.Name, &link.ParentTable, &link.ParentField, &link.ChildTable, &link.ChildField, &link.CreatedAt,
	)
	if err != nil {
		return datamodel.Link{}, fmt.Errorf("get link by id: %w", err)
	}
	return link, nil
}

func (r LinkRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Link, error) {
	query := `
		SELECT id, tenant_id, name, parent_table_id, parent_field_id, child_table_id, child_field_id, created_at
		FROM core.model_links
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()

	var links []datamodel.Link
	for rows.Next() {
		var link datamodel.Link
		if err := rows.Scan(
			&link.ID, &link.TenantID, &link.Name, &link.ParentTable, &link.ParentField, &link.ChildTable, &link.ChildField, &link.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate links: %w", err)
	}
	return links, nil
}

func (r LinkRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM core.model_links WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	return nil
}
