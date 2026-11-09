package mysqlruntime

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type localTenantOperations struct {
	db      *sql.DB
	catalog *store.TenantCatalogReader
	tenants service.TenantService
}

func newLocalTenantHandler(db *sql.DB) (*localTenantHandler, error) {
	catalog, err := store.NewTenantCatalogReader(db, "fraud", localMaxRows, localMaxBytes, localAuthority)
	if err != nil {
		return nil, err
	}
	registration, err := store.NewRegistrationTransactionManager(db, "fraud", localMaxRows, localMaxBytes, localTenantAuthority)
	if err != nil {
		return nil, err
	}
	tenants, err := service.NewTenantService(nil, nil, nil, nil, localIDs{}, localClock{}).WithTenantRegistrationManager(registration)
	if err != nil {
		return nil, err
	}
	return &localTenantHandler{operations: &localTenantOperations{db: db, catalog: catalog, tenants: tenants}}, nil
}

func (o *localTenantOperations) Get(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	var record tenant.Tenant
	err := mysqlstore.Snapshot(ctx, o.db, func(tx *sql.Tx) error {
		if err := localTenantAuthority(ctx, tx, id); err != nil {
			return err
		}
		repositories, err := store.NewMetadataRepositories(tx, "fraud", id, localMaxRows, localMaxBytes)
		if err != nil {
			return err
		}
		record, err = repositories.Tenants.GetByID(ctx, id)
		return err
	})
	return record, err
}
func (o *localTenantOperations) Provision(ctx context.Context, id uuid.UUID) (tenant.Tenant, error) {
	manager, err := store.NewTransactionManager(o.db, "fraud", id, localMaxRows, localMaxBytes, localTenantAuthority, nil)
	if err != nil {
		return tenant.Tenant{}, err
	}
	return manager.ProvisionTenant(ctx, localIDs{})
}
