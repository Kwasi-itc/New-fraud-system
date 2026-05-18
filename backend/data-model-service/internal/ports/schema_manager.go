package ports

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

type SchemaManager interface {
	ProvisionTenantSchema(ctx context.Context, tenant tenant.Tenant) error
	CreateTable(ctx context.Context, tenant tenant.Tenant, table datamodel.Table) error
	DropTable(ctx context.Context, tenant tenant.Tenant, table datamodel.Table) error
	AddField(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, field datamodel.Field) error
	DropField(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, field datamodel.Field) error
	ArchiveField(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, field datamodel.Field) error
	CreateUniqueIndex(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, columns []string) error
	DropUniqueIndex(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, columns []string) error
	CreateManagedIndex(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, job datamodel.IndexJob) error
	GetManagedIndexState(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, job datamodel.IndexJob) (datamodel.ManagedIndexState, error)
}
