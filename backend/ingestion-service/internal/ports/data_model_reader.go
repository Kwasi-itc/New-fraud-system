package ports

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type DataModelReader interface {
	GetPublishedDataModel(ctx context.Context, tenantID uuid.UUID) (ingestion.PublishedDataModel, error)
}
