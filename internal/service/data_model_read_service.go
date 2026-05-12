package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/ports"
)

type DataModelReadService struct {
	repository ports.DataModelReadRepository
}

func NewDataModelReadService(repository ports.DataModelReadRepository) DataModelReadService {
	return DataModelReadService{repository: repository}
}

func (s DataModelReadService) Get(ctx context.Context, tenantID uuid.UUID) (datamodel.AssembledDataModel, error) {
	return s.repository.GetAssembledDataModel(ctx, tenantID)
}
