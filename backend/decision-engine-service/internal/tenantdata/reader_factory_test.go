package tenantdata

import (
	"context"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestRoutingReaderSendsEventTablesToEventRepository(t *testing.T) {
	t.Parallel()
	operational := &countingReader{result: "postgres"}
	events := &countingReader{result: "clickhouse"}
	reader := routingReader{
		dataModels: stubModelReader{model: ports.TenantModel{Tables: map[string]ports.TenantModelTable{
			"accounts":     {Name: "accounts", StorageClass: "operational"},
			"transactions": {Name: "transactions", StorageClass: "event"},
		}}},
		operational: operational,
		events:      events,
	}

	value, err := reader.AggregateRecords(context.Background(), "tenant-1", ports.AggregateQuery{ObjectType: "transactions"})
	if err != nil || value != "clickhouse" || events.calls != 1 || operational.calls != 0 {
		t.Fatalf("event route: value=%v err=%v event_calls=%d operational_calls=%d", value, err, events.calls, operational.calls)
	}
	value, err = reader.AggregateRecords(context.Background(), "tenant-1", ports.AggregateQuery{ObjectType: "accounts"})
	if err != nil || value != "postgres" || events.calls != 1 || operational.calls != 1 {
		t.Fatalf("operational route: value=%v err=%v event_calls=%d operational_calls=%d", value, err, events.calls, operational.calls)
	}
}

func TestNewReaderWithEventsBypassesOperationalRemoteForEventTables(t *testing.T) {
	t.Parallel()
	remote := &countingReader{result: "ingestion"}
	events := &countingReader{result: "clickhouse"}
	models := stubModelReader{model: ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"accounts":     {Name: "accounts", StorageClass: "operational"},
		"transactions": {Name: "transactions", StorageClass: "event"},
	}}}
	reader := NewReaderWithEvents(ReadModeIngestionHTTP, nil, models, remote, events)

	if _, err := reader.AggregateRecords(context.Background(), "tenant-1", ports.AggregateQuery{ObjectType: "transactions"}); err != nil {
		t.Fatal(err)
	}
	if events.calls != 1 || remote.calls != 0 {
		t.Fatalf("event aggregate calls: clickhouse=%d ingestion=%d", events.calls, remote.calls)
	}
	if _, err := reader.AggregateRecords(context.Background(), "tenant-1", ports.AggregateQuery{ObjectType: "accounts"}); err != nil {
		t.Fatal(err)
	}
	if events.calls != 1 || remote.calls != 1 {
		t.Fatalf("operational aggregate calls: clickhouse=%d ingestion=%d", events.calls, remote.calls)
	}
}

type stubModelReader struct{ model ports.TenantModel }

func (s stubModelReader) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return s.model, nil
}
func (stubModelReader) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, nil
}
func (stubModelReader) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, nil
}
func (stubModelReader) RetryIndexJob(context.Context, string) error { return nil }

type countingReader struct {
	result any
	calls  int
}

func (r *countingReader) AggregateRecords(context.Context, string, ports.AggregateQuery) (any, error) {
	r.calls++
	return r.result, nil
}
func (*countingReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}
func (*countingReader) ListRecords(context.Context, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (*countingReader) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
