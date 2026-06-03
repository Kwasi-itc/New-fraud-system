package ports

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type IngestionAuditRepository interface {
	Create(ctx context.Context, audit ingestion.IngestionAudit) error
}

type IdempotencyRepository interface {
	Get(ctx context.Context, tenantID uuid.UUID, key string) (*ingestion.IdempotencyKey, error)
	Create(ctx context.Context, record ingestion.IdempotencyKey) error
}

type OutboxEventRepository interface {
	Create(ctx context.Context, event ingestion.OutboxEvent) error
}

type UploadLogRepository interface {
	Create(ctx context.Context, log ingestion.UploadLog) error
	ListByTenantAndObjectType(ctx context.Context, tenantID uuid.UUID, objectType string) ([]ingestion.UploadLog, error)
	GetByID(ctx context.Context, id uuid.UUID) (ingestion.UploadLog, error)
	Update(ctx context.Context, log ingestion.UploadLog) error
	ClaimNextUploaded(ctx context.Context, now time.Time) (*ingestion.UploadLog, error)
}

type TenantDataWriter interface {
	UpsertRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType string, record map[string]any, mode ingestion.Mode, now time.Time) (string, error)
}

type TenantDataReader interface {
	GetRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType, objectID string) (map[string]any, error)
	ListRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType string, limit int) ([]map[string]any, error)
	QueryRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType, fieldName, value string, limit int) ([]map[string]any, error)
	AggregateRecords(ctx context.Context, model ingestion.PublishedDataModel, query ingestion.AggregateQuery) (any, error)
}

const (
	IdempotencyResponseKindSingle = "single"
	IdempotencyResponseKindBatch  = "batch"
)

type MutationStore interface {
	Audits() IngestionAuditRepository
	Idempotency() IdempotencyRepository
	OutboxEvents() OutboxEventRepository
	UploadLogs() UploadLogRepository
	TenantWriter() TenantDataWriter
	TenantReader() TenantDataReader
}

type TransactionManager interface {
	Run(ctx context.Context, fn func(MutationStore) error) error
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() uuid.UUID
}
