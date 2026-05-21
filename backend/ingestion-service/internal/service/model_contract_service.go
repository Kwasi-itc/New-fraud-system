package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type ModelContractService struct {
	reader ports.DataModelReader
}

func NewModelContractService(reader ports.DataModelReader) ModelContractService {
	return ModelContractService{reader: reader}
}

func (s ModelContractService) Get(ctx context.Context, tenantID uuid.UUID) (ingestion.PublishedDataModel, error) {
	return s.reader.GetPublishedDataModel(ctx, tenantID)
}
