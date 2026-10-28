package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// CatalogAuthorizer verifies trusted platform administration identity and current
// catalogue permission on each read. Use local read-only SQL on the supplied
// snapshot, without network calls, writes or locking reads.
type CatalogAuthorizer func(context.Context, *sql.Tx) error

// TenantCatalogReader owns a bounded read-only snapshot per call. This staged
// reader supplies no tenant lookup or mutation capabilities.
type TenantCatalogReader struct {
	db                *sql.DB
	database          string
	maxRows, maxBytes int
	authorize         CatalogAuthorizer
}

func NewTenantCatalogReader(db *sql.DB, database string, maxRows, maxBytes int, authorize CatalogAuthorizer) (*TenantCatalogReader, error) {
	if _, err := mysqlstore.Identifier(database); err != nil {
		return nil, err
	}
	if db == nil || maxRows < 1 || maxRows > 100000 || maxBytes < 1 || maxBytes > 64<<20 || authorize == nil {
		return nil, errors.New("database, bounded catalogue limits and authorizer required")
	}
	return &TenantCatalogReader{db, database, maxRows, maxBytes, authorize}, nil
}

func (r *TenantCatalogReader) List(ctx context.Context) ([]tenant.Tenant, error) {
	if r == nil || r.db == nil || r.authorize == nil {
		return nil, errors.New("initialized tenant catalogue reader required")
	}
	var result []tenant.Tenant
	err := mysqlstore.Snapshot(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.authorize(ctx, tx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		catalog, err := NewTenantCatalog(tx, r.database, r.maxRows, r.maxBytes)
		if err != nil {
			return err
		}
		result, err = catalog.List(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

var _ ports.TenantCatalog = (*TenantCatalogReader)(nil)
