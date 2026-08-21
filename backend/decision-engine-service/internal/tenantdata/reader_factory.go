package tenantdata

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
)

const (
	ReadModeIngestionHTTP = "ingestion_http"
	ReadModeDirectDB      = "direct_db"
)

func NewReader(readMode string, db *pgxpool.Pool, dataModelReader ports.DataModelReader, ingestionServiceURL string, timeout time.Duration) ports.TenantDataReader {
	remote := ingestionclient.NewHTTPClient(ingestionServiceURL, timeout)
	return NewReaderWithEvents(readMode, db, dataModelReader, remote, remote)
}

// NewReaderWithEvents keeps operational-model routing configurable while
// always routing event models to their ClickHouse repository reader.
func NewReaderWithEvents(readMode string, db *pgxpool.Pool, dataModelReader ports.DataModelReader, remote, events ports.TenantDataReader) ports.TenantDataReader {
	operational := ports.TenantDataReader(remote)
	switch strings.ToLower(strings.TrimSpace(readMode)) {
	case ReadModeDirectDB:
		if db != nil {
			operational = storepostgres.NewTenantDataReader(db, dataModelReader)
		}
	}
	if events == nil {
		events = remote
	}
	return routingReader{dataModels: dataModelReader, operational: operational, events: events}
}

type routingReader struct {
	dataModels  ports.DataModelReader
	operational ports.TenantDataReader
	events      ports.TenantDataReader
}

func (r routingReader) reader(ctx context.Context, tenantID, objectType string) (ports.TenantDataReader, error) {
	model, err := r.dataModels.GetTenantModel(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if table, ok := model.Tables[objectType]; ok && table.StorageClass == "event" {
		return r.events, nil
	}
	return r.operational, nil
}

func (r routingReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	reader, err := r.reader(ctx, tenantID, objectType)
	if err != nil {
		return ports.TenantRecord{}, err
	}
	return reader.GetRecord(ctx, tenantID, objectType, objectID)
}

func (r routingReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	reader, err := r.reader(ctx, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	return reader.ListRecords(ctx, tenantID, objectType, limit)
}

func (r routingReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	reader, err := r.reader(ctx, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	return reader.QueryRecords(ctx, tenantID, objectType, fieldName, value, limit)
}

func (r routingReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	reader, err := r.reader(ctx, tenantID, query.ObjectType)
	if err != nil {
		return nil, err
	}
	return reader.AggregateRecords(ctx, tenantID, query)
}

func (r routingReader) BatchAggregateRecords(ctx context.Context, tenantID string, queries []ports.AggregateQuery) ([]any, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	reader, err := r.reader(ctx, tenantID, queries[0].ObjectType)
	if err != nil {
		return nil, err
	}
	if batchReader, ok := reader.(ports.BatchTenantDataReader); ok {
		return batchReader.BatchAggregateRecords(ctx, tenantID, queries)
	}
	values := make([]any, len(queries))
	for i, query := range queries {
		value, err := reader.AggregateRecords(ctx, tenantID, query)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}
