package clickhouse

import (
	"context"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	sharedeventstore "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service"
)

func TestAggregateBuildsTypedEventContract(t *testing.T) {
	store := &capturingEventRepository{value: float64(12500)}
	models := eventModelReader{model: ports.TenantModel{
		RevisionID: "model-revision",
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Name: "transactions", StorageClass: "event",
				EventTimeField: "date", EventSchemaRevision: "schema-revision",
				Fields: map[string]ports.TenantModelField{
					"object_id": {Name: "object_id", Type: "string"},
					"date":      {Name: "date", Type: "timestamp"},
					"amount":    {Name: "amount", Type: "float", Nullable: true},
				},
			},
		},
	}}
	reader := NewTenantDataReader(store, models)
	value, err := reader.AggregateRecords(context.Background(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ports.AggregateQuery{
		ObjectType: "transactions", Aggregate: "sum", Field: "amount",
		Filter: &ports.AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-08-20T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != float64(12500) {
		t.Fatalf("AggregateRecords() value = %#v", value)
	}
	request := store.aggregateRequest
	if request.Table.SchemaRevision != "schema-revision" || request.Table.EventTimeField != "date" {
		t.Fatalf("event contract = %+v", request.Table)
	}
	if field := request.Table.Fields["amount"]; field.DataType != "float" || !field.Nullable {
		t.Fatalf("amount field contract = %+v", field)
	}
	if request.Filter == nil || request.Filter.Field != "date" {
		t.Fatalf("aggregate filter = %+v", request.Filter)
	}
}

type capturingEventRepository struct {
	value            any
	aggregateRequest sharedeventstore.AggregateRequest
}

func (*capturingEventRepository) GetRecord(context.Context, sharedeventstore.RecordRequest) (map[string]any, error) {
	return nil, nil
}
func (*capturingEventRepository) ListRecords(context.Context, sharedeventstore.RecordRequest) ([]map[string]any, error) {
	return nil, nil
}
func (r *capturingEventRepository) Aggregate(_ context.Context, request sharedeventstore.AggregateRequest) (any, error) {
	r.aggregateRequest = request
	return r.value, nil
}

type eventModelReader struct{ model ports.TenantModel }

func (r eventModelReader) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return r.model, nil
}
func (eventModelReader) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, nil
}
func (eventModelReader) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, nil
}
func (eventModelReader) RetryIndexJob(context.Context, string) error { return nil }
