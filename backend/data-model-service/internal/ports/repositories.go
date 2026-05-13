package ports

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

type TenantRepository interface {
	Create(ctx context.Context, tenant tenant.Tenant) error
	GetByID(ctx context.Context, id uuid.UUID) (tenant.Tenant, error)
	List(ctx context.Context) ([]tenant.Tenant, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status tenant.Status) error
}
