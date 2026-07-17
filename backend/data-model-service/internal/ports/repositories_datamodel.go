package ports

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type TableRepository interface {
	Create(ctx context.Context, table datamodel.Table) error
	GetByID(ctx context.Context, id uuid.UUID) (datamodel.Table, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Table, error)
	Update(ctx context.Context, table datamodel.Table) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type FieldRepository interface {
	Create(ctx context.Context, field datamodel.Field) error
	GetByID(ctx context.Context, id uuid.UUID) (datamodel.Field, error)
	ListByTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.Field, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Update(ctx context.Context, field datamodel.Field) error
}

type FieldEnumValueRepository interface {
	Create(ctx context.Context, value datamodel.FieldEnumValue) error
	GetByID(ctx context.Context, id uuid.UUID) (datamodel.FieldEnumValue, error)
	ListByField(ctx context.Context, fieldID uuid.UUID) ([]datamodel.FieldEnumValue, error)
	Update(ctx context.Context, value datamodel.FieldEnumValue) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type LinkRepository interface {
	Create(ctx context.Context, link datamodel.Link) error
	GetByID(ctx context.Context, id uuid.UUID) (datamodel.Link, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Link, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type PivotRepository interface {
	Create(ctx context.Context, pivot datamodel.Pivot) error
	GetByID(ctx context.Context, id uuid.UUID) (datamodel.Pivot, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Pivot, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type TableOptionsRepository interface {
	GetByTableID(ctx context.Context, tableID uuid.UUID) (*datamodel.TableOptions, error)
	Upsert(ctx context.Context, options datamodel.TableOptions) error
}

type NavigationOptionRepository interface {
	Create(ctx context.Context, option datamodel.NavigationOption) error
	GetByID(ctx context.Context, id uuid.UUID) (datamodel.NavigationOption, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.NavigationOption, error)
	ListBySourceTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.NavigationOption, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type DataModelReadRepository interface {
	GetAssembledDataModel(ctx context.Context, tenantID uuid.UUID) (datamodel.AssembledDataModel, error)
}

type SchemaChangeRepository interface {
	Create(ctx context.Context, change datamodel.SchemaChange) error
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.SchemaChange, error)
}

type TenantSchemaMigrationRepository interface {
	Create(ctx context.Context, migration datamodel.TenantSchemaMigration) error
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.TenantSchemaMigration, error)
}

type IndexJobRepository interface {
	Create(ctx context.Context, job datamodel.IndexJob) error
	GetByID(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.IndexJob, error)
	StartAttempt(ctx context.Context, id uuid.UUID, startedAt time.Time) (datamodel.IndexJob, error)
	MarkApplied(ctx context.Context, id uuid.UUID, completedAt time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, message string, completedAt time.Time) error
	MarkPendingRetry(ctx context.Context, id uuid.UUID, message string) error
	Retry(ctx context.Context, id uuid.UUID, scheduledAt time.Time) error
}
