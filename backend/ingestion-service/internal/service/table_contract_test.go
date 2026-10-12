package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
)

type scopedContractFailure struct {
	t      *testing.T
	tenant uuid.UUID
	err    error
	calls  int
}

func (r *scopedContractFailure) GetPublishedDataModel(context.Context, uuid.UUID) (ingestion.PublishedDataModel, error) {
	r.t.Fatal("table operation requested a full model")
	return ingestion.PublishedDataModel{}, r.err
}
func (r *scopedContractFailure) GetPublishedTable(_ context.Context, tenant uuid.UUID, objectType string) (ingestion.PublishedDataModel, error) {
	r.calls++
	if tenant != r.tenant || objectType != "records" {
		r.t.Fatal("table contract scope mismatch")
	}
	return ingestion.PublishedDataModel{}, r.err
}

func TestAllRecordOperationsUseScopedContractAndPropagateFailure(t *testing.T) {
	ctx := context.Background()
	reader := &scopedContractFailure{t: t, tenant: uuid.New(), err: errors.New("contract unavailable")}
	svc := IngestService{dataModelReader: reader}
	checks := []func() error{
		func() error {
			_, _, err := svc.Ingest(ctx, IngestInput{TenantID: reader.tenant, ObjectType: "records"})
			return err
		},
		func() error {
			_, _, err := svc.BatchIngest(ctx, BatchIngestInput{TenantID: reader.tenant, ObjectType: "records"})
			return err
		},
		func() error { _, err := svc.GetRecord(ctx, reader.tenant, "records", "one"); return err },
		func() error { _, err := svc.ListRecords(ctx, reader.tenant, "records", 10); return err },
		func() error { _, err := svc.QueryRecords(ctx, reader.tenant, "records", "amount", "1", 10); return err },
		func() error {
			_, err := svc.AggregateRecords(ctx, reader.tenant, ingestion.AggregateQuery{ObjectType: "records"})
			return err
		},
	}
	for _, check := range checks {
		if err := check(); !errors.Is(err, reader.err) {
			t.Fatalf("dependency failure swallowed: %v", err)
		}
	}
	if reader.calls != len(checks) {
		t.Fatalf("lookups=%d", reader.calls)
	}
}
