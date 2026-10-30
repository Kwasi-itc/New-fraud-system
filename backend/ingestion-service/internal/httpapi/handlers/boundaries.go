package handlers

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	"github.com/google/uuid"
)

type IngestWriter interface {
	Ingest(context.Context, service.IngestInput) (ingestion.RecordResult, []ingestion.ValidationError, error)
	BatchIngest(context.Context, service.BatchIngestInput) ([]ingestion.RecordResult, []ingestion.ValidationError, error)
}
type UploadService interface {
	Create(context.Context, uuid.UUID, string, ingestion.Mode, string, string, []byte) (ingestion.UploadLog, error)
	Get(context.Context, uuid.UUID) (ingestion.UploadLog, error)
	List(context.Context, uuid.UUID, string) ([]ingestion.UploadLog, error)
}
type DeferredService interface {
	Create(context.Context, uuid.UUID, string, ingestion.Mode, map[string]any, *string) (ingestion.DeferredIngest, error)
	Get(context.Context, uuid.UUID) (ingestion.DeferredIngest, error)
}

// IngestReader supplies the existing bounded read transport contract.
type IngestReader interface {
	GetRecord(context.Context, uuid.UUID, string, string) (service.RecordLookupResult, error)
	ListRecords(context.Context, uuid.UUID, string, int) (service.RecordListResult, error)
	QueryRecords(context.Context, uuid.UUID, string, string, string, int) (service.RecordQueryResult, error)
	AggregateRecords(context.Context, uuid.UUID, ingestion.AggregateQuery) (service.AggregateResult, error)
}
