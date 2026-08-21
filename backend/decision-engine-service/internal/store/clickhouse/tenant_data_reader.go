package clickhouse

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	sharedeventstore "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service"
)

// TenantDataReader adapts the shared event repository to the decision engine's
// tenant-data port. Event data never traverses ingestion-service HTTP.
type TenantDataReader struct {
	store      eventRepository
	dataModels ports.DataModelReader
}

type eventRepository interface {
	GetRecord(context.Context, sharedeventstore.RecordRequest) (map[string]any, error)
	ListRecords(context.Context, sharedeventstore.RecordRequest) ([]map[string]any, error)
	Aggregate(context.Context, sharedeventstore.AggregateRequest) (any, error)
}

type eventBatchRepository interface {
	AggregateBatch(context.Context, []sharedeventstore.AggregateRequest) ([]any, error)
}

func NewTenantDataReader(store eventRepository, dataModels ports.DataModelReader) TenantDataReader {
	return TenantDataReader{store: store, dataModels: dataModels}
}

func (r TenantDataReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	table, err := r.resolveTable(ctx, tenantID, objectType)
	if err != nil {
		return ports.TenantRecord{}, err
	}
	record, err := r.store.GetRecord(ctx, sharedeventstore.RecordRequest{Table: table, ObjectID: objectID})
	if err != nil {
		return ports.TenantRecord{}, err
	}
	return recordEnvelope(objectType, record), nil
}

func (r TenantDataReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	table, err := r.resolveTable(ctx, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	records, err := r.store.ListRecords(ctx, sharedeventstore.RecordRequest{Table: table, Limit: limit})
	if err != nil {
		return nil, err
	}
	return recordEnvelopes(objectType, records), nil
}

func (r TenantDataReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	table, err := r.resolveTable(ctx, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	records, err := r.store.ListRecords(ctx, sharedeventstore.RecordRequest{Table: table, Field: fieldName, Value: value, Limit: limit})
	if err != nil {
		return nil, err
	}
	return recordEnvelopes(objectType, records), nil
}

func (r TenantDataReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	table, err := r.resolveTable(ctx, tenantID, query.ObjectType)
	if err != nil {
		return nil, err
	}
	return r.store.Aggregate(ctx, sharedeventstore.AggregateRequest{
		Table: table, Aggregate: query.Aggregate, Field: query.Field, Filter: convertFilter(query.Filter),
	})
}

func (r TenantDataReader) BatchAggregateRecords(ctx context.Context, tenantID string, queries []ports.AggregateQuery) ([]any, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	table, err := r.resolveTable(ctx, tenantID, queries[0].ObjectType)
	if err != nil {
		return nil, err
	}
	requests := make([]sharedeventstore.AggregateRequest, len(queries))
	for i, query := range queries {
		if query.ObjectType != queries[0].ObjectType {
			return nil, fmt.Errorf("aggregate batch must target one object type")
		}
		requests[i] = sharedeventstore.AggregateRequest{
			Table: table, Aggregate: query.Aggregate, Field: query.Field, Filter: convertFilter(query.Filter),
		}
	}
	if batchStore, ok := r.store.(eventBatchRepository); ok {
		return batchStore.AggregateBatch(ctx, requests)
	}
	values := make([]any, len(requests))
	for i, request := range requests {
		value, err := r.store.Aggregate(ctx, request)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

func (r TenantDataReader) resolveTable(ctx context.Context, tenantID, objectType string) (sharedeventstore.TableContract, error) {
	model, err := r.dataModels.GetTenantModel(ctx, tenantID)
	if err != nil {
		return sharedeventstore.TableContract{}, err
	}
	table, ok := model.Tables[objectType]
	if !ok {
		return sharedeventstore.TableContract{}, fmt.Errorf("object type %s is not available", objectType)
	}
	if table.StorageClass != "event" {
		return sharedeventstore.TableContract{}, fmt.Errorf("object type %s is not an event table", objectType)
	}
	if strings.TrimSpace(table.EventSchemaRevision) == "" {
		return sharedeventstore.TableContract{}, fmt.Errorf("event table %s is missing event_schema_revision", objectType)
	}
	fields := make(map[string]sharedeventstore.FieldContract, len(table.Fields))
	for name, field := range table.Fields {
		fields[name] = sharedeventstore.FieldContract{
			DataType: field.Type, Nullable: field.Nullable, IsProjection: field.IsProjection,
			AggregationMode: field.AggregationMode, AggregationColdBehavior: field.AggregationColdBehavior,
			AggregationDefaultValue: field.AggregationDefaultValue,
		}
	}
	return sharedeventstore.TableContract{
		TenantID: tenantID, TableID: table.ID, ObjectType: objectType,
		RevisionID: model.RevisionID, SchemaRevision: table.EventSchemaRevision,
		EventTimeField: table.EventTimeField, Fields: fields,
	}, nil
}

func recordEnvelope(objectType string, fields map[string]any) ports.TenantRecord {
	return ports.TenantRecord{ObjectID: fmt.Sprint(fields["object_id"]), ObjectType: objectType, Fields: fields}
}

func recordEnvelopes(objectType string, records []map[string]any) []ports.TenantRecord {
	out := make([]ports.TenantRecord, len(records))
	for i, record := range records {
		out[i] = recordEnvelope(objectType, record)
	}
	return out
}

func convertFilter(filter *ports.AggregateFilter) *sharedeventstore.AggregateFilter {
	if filter == nil {
		return nil
	}
	converted := &sharedeventstore.AggregateFilter{
		Kind: filter.Kind, Operator: filter.Operator, Field: filter.Field, Op: filter.Op, Value: filter.Value,
		Children: make([]sharedeventstore.AggregateFilter, len(filter.Children)),
	}
	for i := range filter.Children {
		converted.Children[i] = *convertFilter(&filter.Children[i])
	}
	return converted
}
