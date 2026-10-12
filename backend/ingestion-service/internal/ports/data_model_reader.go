package ports

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type DataModelReader interface {
	GetPublishedDataModel(ctx context.Context, tenantID uuid.UUID) (ingestion.PublishedDataModel, error)
}

// TableDataModelReader avoids full-tenant reads on the ingestion and record paths.
// Legacy HTTP readers remain supported during the migration rollout.
type TableDataModelReader interface {
	GetPublishedTable(ctx context.Context, tenantID uuid.UUID, objectType string) (ingestion.PublishedDataModel, error)
}

type PublishedContract struct {
	TenantID     uuid.UUID
	TableID      uuid.UUID
	TableName    string
	SchemaName   string
	TenantStatus string
	Version      uuid.UUID
	Payload      []byte // nil only when version/table match, or the table does not exist
}

type IngestionContractStore interface {
	ReadContract(ctx context.Context, tenantID uuid.UUID, objectType string, knownTable uuid.UUID, knownVersion uuid.UUID) (PublishedContract, error)
}
