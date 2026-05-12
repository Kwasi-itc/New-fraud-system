package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/tenant"
)

type TenantRepository struct {
	db executor
}

func NewTenantRepository(db executor) TenantRepository {
	return TenantRepository{db: db}
}

func (r TenantRepository) Create(ctx context.Context, record tenant.Tenant) error {
	query := `
		INSERT INTO core.tenants (id, external_key, name, schema_name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.Exec(
		ctx,
		query,
		record.ID,
		record.ExternalKey,
		record.Name,
		record.SchemaName,
		record.Status,
		record.CreatedAt,
		record.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}

	return nil
}

func (r TenantRepository) GetByID(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	query := `
		SELECT id, external_key, name, schema_name, status, created_at, updated_at
		FROM core.tenants
		WHERE id = $1
	`

	var record tenant.Tenant
	err := r.db.QueryRow(ctx, query, id).Scan(
		&record.ID,
		&record.ExternalKey,
		&record.Name,
		&record.SchemaName,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return tenant.Tenant{}, fmt.Errorf("get tenant by id: %w", err)
	}

	return record, nil
}

func (r TenantRepository) List(ctx context.Context) ([]tenant.Tenant, error) {
	query := `
		SELECT id, external_key, name, schema_name, status, created_at, updated_at
		FROM core.tenants
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	records := make([]tenant.Tenant, 0)
	for rows.Next() {
		var record tenant.Tenant
		if err := rows.Scan(
			&record.ID,
			&record.ExternalKey,
			&record.Name,
			&record.SchemaName,
			&record.Status,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}

	return records, nil
}

func (r TenantRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status tenant.Status) error {
	query := `
		UPDATE core.tenants
		SET status = $2, updated_at = NOW()
		WHERE id = $1
	`

	_, err := r.db.Exec(ctx, query, id, status)
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}

	return nil
}
