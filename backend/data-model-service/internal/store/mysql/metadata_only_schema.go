package mysql

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

var ErrPhysicalProtocolRequired = errors.New("physical operations require the separate durable DDL and readiness protocol")

// This deliberately has no database handle. Legacy services must not execute
// MySQL DDL (or infer readiness) through their metadata transaction interface.
type metadataOnlySchemaManager struct{}

func (metadataOnlySchemaManager) ProvisionTenantSchema(context.Context, tenant.Tenant) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) CreateTable(context.Context, tenant.Tenant, datamodel.Table) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) DropTable(context.Context, tenant.Tenant, datamodel.Table) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) AddField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) DropField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) ArchiveField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) CreateUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) DropUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) CreateManagedIndex(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) error {
	return ErrPhysicalProtocolRequired
}
func (metadataOnlySchemaManager) GetManagedIndexState(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) (datamodel.ManagedIndexState, error) {
	return datamodel.ManagedIndexState{}, ErrPhysicalProtocolRequired
}
