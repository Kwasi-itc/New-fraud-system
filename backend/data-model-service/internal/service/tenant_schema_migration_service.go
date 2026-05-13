package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type TenantSchemaMigrationService struct {
	repository ports.TenantSchemaMigrationRepository
}

func NewTenantSchemaMigrationService(repository ports.TenantSchemaMigrationRepository) TenantSchemaMigrationService {
	return TenantSchemaMigrationService{repository: repository}
}

func (s TenantSchemaMigrationService) List(ctx context.Context, tenantID uuid.UUID) ([]datamodel.TenantSchemaMigration, error) {
	return s.repository.ListByTenant(ctx, tenantID)
}
