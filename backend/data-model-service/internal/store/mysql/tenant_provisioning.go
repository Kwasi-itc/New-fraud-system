package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// ProvisionTenant activates a tenant in an already installed shared MySQL
// database. The deployment must verify owner migrations and grants before
// accepting requests. It does not create a per-tenant database or publish any
// business tables; those require the separate physical creation protocol.
// Locking the exact tenant row makes retries (including after lost responses)
// return the committed result without duplicating history or revisions.
func (m *TransactionManager) ProvisionTenant(ctx context.Context, ids ports.IDGenerator) (tenant.Tenant, error) {
	if m == nil || ids == nil {
		return tenant.Tenant{}, errors.New("provisioning dependencies required")
	}
	var result tenant.Tenant
	err := m.RunWithTx(ctx, func(owner ports.MutationStore, tx *sql.Tx) error {
		schema, err := mysqlstore.Identifier(m.database)
		if err != nil {
			return err
		}
		var locked mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+schema+".data_model_tenants WHERE id=? FOR UPDATE", mysqlstore.UUID(m.tenant)).Scan(&locked); err != nil {
			return err
		}
		record, err := owner.Tenants().GetByID(ctx, m.tenant)
		if err != nil {
			return err
		}
		if record.Status == tenant.StatusActive {
			result = record
			return nil
		}
		if record.Status != tenant.StatusPending {
			return errors.New("tenant cannot be provisioned from current state")
		}
		if err := owner.Tenants().UpdateStatus(ctx, m.tenant, tenant.StatusActive); err != nil {
			return err
		}
		record, err = owner.Tenants().GetByID(ctx, m.tenant)
		if err != nil {
			return err
		}
		details, err := json.Marshal(map[string]string{"database": m.database, "schema_name": record.SchemaName, "storage_layout": "shared_mysql"})
		if err != nil {
			return err
		}
		if err := owner.SchemaChanges().Create(ctx, datamodel.SchemaChange{ID: ids.New(), TenantID: m.tenant, Operation: "provision_tenant_schema", ResourceType: "tenant", ResourceID: m.tenant, Status: "applied", Details: details, CreatedAt: record.UpdatedAt}); err != nil {
			return err
		}
		if err := owner.TenantSchemaMigrations().Create(ctx, datamodel.TenantSchemaMigration{ID: ids.New(), TenantID: m.tenant, Version: "provision_tenant_schema:tenant", AppliedAt: record.UpdatedAt}); err != nil {
			return err
		}
		result = record
		return nil
	})
	if err != nil {
		return tenant.Tenant{}, err
	}
	return result, nil
}
