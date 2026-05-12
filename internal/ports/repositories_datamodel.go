package ports

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
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

type DataModelReadRepository interface {
	GetAssembledDataModel(ctx context.Context, tenantID uuid.UUID) (datamodel.AssembledDataModel, error)
}

type SchemaChangeRepository interface {
	Create(ctx context.Context, change datamodel.SchemaChange) error
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.SchemaChange, error)
}
