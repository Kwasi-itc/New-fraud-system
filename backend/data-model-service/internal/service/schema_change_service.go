package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type SchemaChangeService struct {
	repository ports.SchemaChangeRepository
}

func NewSchemaChangeService(repository ports.SchemaChangeRepository) SchemaChangeService {
	return SchemaChangeService{repository: repository}
}

func (s SchemaChangeService) List(ctx context.Context, tenantID uuid.UUID) ([]datamodel.SchemaChange, error) {
	return s.repository.ListByTenant(ctx, tenantID)
}

