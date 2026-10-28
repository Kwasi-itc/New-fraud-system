package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// TenantRepository is bound to one already authorized tenant, including during
// registration before the row exists. It cannot enumerate the global catalogue.
type TenantRepository struct{ metadataRepository }

func (r TenantRepository) Create(ctx context.Context, v tenant.Tenant) error {
	external := sql.NullString{}
	if v.ExternalKey != nil {
		external = sql.NullString{String: *v.ExternalKey, Valid: true}
	}
	if err := r.validate(v.ID, v.ID, external.String, v.Name, v.SchemaName, string(v.Status)); err != nil {
		return err
	}
	if err := validTenantStatus(v.Status); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_tenants(id,external_key,name,schema_name,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), external, v.Name, v.SchemaName, v.Status, mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt})
	return err
}

func validTenantStatus(status tenant.Status) error {
	if status != tenant.StatusPending && status != tenant.StatusActive {
		return errors.New("invalid tenant status")
	}
	return nil
}

func tenantSelect(r metadataRepository) string {
	return "SELECT id,external_key IS NOT NULL," + r.projection("COALESCE(external_key,'')", "name", "schema_name", "status") + ",created_at,updated_at FROM " + r.schema + ".data_model_tenants"
}

func scanTenant(row metadataScanner) (tenant.Tenant, int64, error) {
	var v tenant.Tenant
	var present bool
	var external string
	var size int64
	var created, updated mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), &present, &size, &external, &v.Name, &v.SchemaName, &v.Status, &created, &updated)
	if present {
		v.ExternalKey = &external
	}
	v.CreatedAt, v.UpdatedAt = created.Time, updated.Time
	return v, size, err
}

func (r TenantRepository) GetByID(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	if id != r.tenant {
		return tenant.Tenant{}, sql.ErrNoRows
	}
	return metadataOne(r.metadataRepository, r.tx.QueryRowContext(ctx, tenantSelect(r.metadataRepository)+" WHERE id=?", mysqlstore.UUID(r.tenant)), scanTenant)
}

// List preserves the repository's tenant scope. Platform-wide listing is only
// available on TenantCatalog, never by passing a zero tenant to this factory.
func (r TenantRepository) List(ctx context.Context) ([]tenant.Tenant, error) {
	return metadataList(ctx, r.metadataRepository, tenantSelect(r.metadataRepository)+" WHERE id=?", []any{mysqlstore.UUID(r.tenant)}, scanTenant)
}

func (r TenantRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status tenant.Status) error {
	if id != r.tenant {
		return sql.ErrNoRows
	}
	if err := validTenantStatus(status); err != nil {
		return err
	}
	var found mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".data_model_tenants WHERE id=? FOR UPDATE", mysqlstore.UUID(id)).Scan(&found); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_tenants SET status=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?", status, mysqlstore.UUID(id))
	return err
}

// TenantCatalog is a platform administration read adapter, not an authorization
// boundary. Construct only after authorizing catalogue access. It exposes no
// mutation methods and must not replace tenant-scoped lookup in owner services.
type TenantCatalog struct{ repository metadataRepository }
type TenantCatalogCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func NewTenantCatalog(tx *sql.Tx, database string, maxRows, maxBytes int) (TenantCatalog, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return TenantCatalog{}, err
	}
	if tx == nil || maxRows < 1 || maxRows > 100000 || maxBytes < 1 || maxBytes > 64<<20 {
		return TenantCatalog{}, errors.New("transaction and bounded row/byte limits required")
	}
	return TenantCatalog{metadataRepository{tx: tx, schema: schema, maxRows: maxRows, maxBytes: maxBytes}}, nil
}

func (c TenantCatalog) List(ctx context.Context) ([]tenant.Tenant, error) {
	r := c.repository
	return metadataList(ctx, r, tenantSelect(r)+" ORDER BY created_at DESC,id DESC", nil, scanTenant)
}

// ListPage is keyset paging within the caller's transaction. Cross-request
// consistency requires an explicit snapshot/export protocol, not just a cursor.
func (c TenantCatalog) ListPage(ctx context.Context, limit int, after *TenantCatalogCursor) ([]tenant.Tenant, *TenantCatalogCursor, error) {
	r := c.repository
	if limit < 1 || limit > r.maxRows {
		return nil, nil, errors.New("catalogue page limit outside configured budget")
	}
	query := tenantSelect(r)
	args := []any{}
	if after != nil {
		if after.ID == uuid.Nil {
			return nil, nil, errors.New("catalogue cursor identity required")
		}
		at := mysqlstore.DateTime{Time: after.CreatedAt}
		if _, err := at.Value(); err != nil {
			return nil, nil, err
		}
		query += " WHERE created_at<? OR (created_at=? AND id<?)"
		args = append(args, at, at, mysqlstore.UUID(after.ID))
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	values := make([]tenant.Tenant, 0)
	var used int64
	var more bool
	for rows.Next() {
		if len(values) == limit {
			more = true
			continue
		}
		v, size, err := scanTenant(rows)
		if err != nil {
			return nil, nil, err
		}
		if size > int64(r.maxBytes)-used {
			return nil, nil, ErrSnapshotLimit
		}
		used += size
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if more {
		last := values[len(values)-1]
		return values, &TenantCatalogCursor{CreatedAt: last.CreatedAt, ID: last.ID}, nil
	}
	return values, nil, nil
}
