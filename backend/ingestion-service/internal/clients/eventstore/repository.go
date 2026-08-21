package eventstore

import (
	"context"
	"fmt"
	"time"

	sharedeventstore "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

// Repository adapts the shared ClickHouse repository to ingestion's storage
// port. It deliberately contains no HTTP client or service-to-service hop.
type Repository struct {
	store *sharedeventstore.Repository
}

func NewRepository(store *sharedeventstore.Repository) Repository {
	return Repository{store: store}
}

func (r Repository) Write(ctx context.Context, model ingestion.PublishedDataModel, objectType, eventID, objectID, requestHash string, payload map[string]any, ingestedAt time.Time) error {
	table, err := buildTableContract(model, objectType)
	if err != nil {
		return err
	}
	event, err := buildEvent(model, objectType, ports.EventWrite{EventID: eventID, ObjectID: objectID, RequestHash: requestHash, Payload: payload, IngestedAt: ingestedAt})
	if err != nil {
		return err
	}
	return r.store.Write(ctx, table, event)
}

func (r Repository) WriteBatch(ctx context.Context, model ingestion.PublishedDataModel, objectType string, writes []ports.EventWrite) error {
	table, err := buildTableContract(model, objectType)
	if err != nil {
		return err
	}
	events := make([]sharedeventstore.Event, len(writes))
	for i, write := range writes {
		event, err := buildEvent(model, objectType, write)
		if err != nil {
			return err
		}
		events[i] = event
	}
	return r.store.WriteBatch(ctx, table, events)
}

func buildEvent(model ingestion.PublishedDataModel, objectType string, write ports.EventWrite) (sharedeventstore.Event, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return sharedeventstore.Event{}, fmt.Errorf("object type %s is not available", objectType)
	}
	raw, ok := write.Payload[table.EventTimeField]
	if !ok {
		return sharedeventstore.Event{}, fmt.Errorf("event time field %s is required", table.EventTimeField)
	}
	eventTime, ok := raw.(time.Time)
	if !ok {
		return sharedeventstore.Event{}, fmt.Errorf("event time field %s is not a normalized timestamp", table.EventTimeField)
	}
	return sharedeventstore.Event{EventID: write.EventID, ObjectID: write.ObjectID, EventTime: eventTime, IngestedAt: write.IngestedAt, RequestHash: write.RequestHash, Payload: write.Payload}, nil
}

func (r Repository) GetRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType, objectID string) (map[string]any, error) {
	table, err := buildTableContract(model, objectType)
	if err != nil {
		return nil, err
	}
	return r.store.GetRecord(ctx, sharedeventstore.RecordRequest{Table: table, ObjectID: objectID})
}

func (r Repository) ListRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType string, limit int) ([]map[string]any, error) {
	return r.list(ctx, model, objectType, "", "", limit)
}

func (r Repository) QueryRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType, fieldName, value string, limit int) ([]map[string]any, error) {
	return r.list(ctx, model, objectType, fieldName, value, limit)
}

func (r Repository) list(ctx context.Context, model ingestion.PublishedDataModel, objectType, field, value string, limit int) ([]map[string]any, error) {
	table, err := buildTableContract(model, objectType)
	if err != nil {
		return nil, err
	}
	return r.store.ListRecords(ctx, sharedeventstore.RecordRequest{Table: table, Field: field, Value: value, Limit: limit})
}

func (r Repository) AggregateRecords(ctx context.Context, model ingestion.PublishedDataModel, query ingestion.AggregateQuery) (any, error) {
	table, err := buildTableContract(model, query.ObjectType)
	if err != nil {
		return nil, err
	}
	return r.store.Aggregate(ctx, sharedeventstore.AggregateRequest{
		Table: table, Aggregate: query.Aggregate, Field: query.Field, Filter: convertFilter(query.Filter),
	})
}

func buildTableContract(model ingestion.PublishedDataModel, objectType string) (sharedeventstore.TableContract, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return sharedeventstore.TableContract{}, fmt.Errorf("object type %s is not available", objectType)
	}
	if table.StorageClass != "event" {
		return sharedeventstore.TableContract{}, fmt.Errorf("object type %s is not an event table", objectType)
	}
	fields := make(map[string]sharedeventstore.FieldContract, len(table.Fields))
	for name, field := range table.Fields {
		if field.Archived {
			continue
		}
		fields[name] = sharedeventstore.FieldContract{
			DataType: field.DataType, Nullable: field.Nullable, IsProjection: field.IsProjection,
			AggregationMode: field.AggregationMode, AggregationColdBehavior: field.AggregationColdBehavior,
			AggregationDefaultValue: field.AggregationDefaultValue,
		}
	}
	return sharedeventstore.TableContract{
		TenantID:       model.TenantID.String(),
		TableID:        table.ID.String(),
		ObjectType:     objectType,
		RevisionID:     model.RevisionID,
		SchemaRevision: table.EventSchemaRevision,
		EventTimeField: table.EventTimeField,
		Fields:         fields,
	}, nil
}

func convertFilter(filter *ingestion.AggregateFilter) *sharedeventstore.AggregateFilter {
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
