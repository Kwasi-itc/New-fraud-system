package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

func TestGetRecordUsesConfiguredReadDataReader(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := NewIngestService(
		stubPublishedModelReader{
			model: publishedTransactionsModel(tenantID),
		},
		panicTransactionManager{},
		stubReadDataReader{
			record: map[string]any{
				"object_id": "txn-1",
				"amount":    42.0,
				"status":    "pending",
			},
		},
		&fixedIDGenerator{},
		fixedClock{now: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)},
	)

	result, err := service.GetRecord(context.Background(), tenantID, "transactions", "txn-1")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if result.ObjectID != "txn-1" {
		t.Fatalf("GetRecord() object_id = %q, want txn-1", result.ObjectID)
	}
	if result.Fields["status"] != "pending" {
		t.Fatalf("GetRecord() fields = %#v, want pending status", result.Fields)
	}
}

type panicTransactionManager struct{}

func (panicTransactionManager) Run(context.Context, func(ports.MutationStore) error) error {
	panic("write transaction manager should not be used for read-path request")
}

type stubReadDataReader struct {
	record map[string]any
}

func (s stubReadDataReader) GetRecord(context.Context, ingestion.PublishedDataModel, string, string) (map[string]any, error) {
	return s.record, nil
}

func (stubReadDataReader) ListRecords(context.Context, ingestion.PublishedDataModel, string, int) ([]map[string]any, error) {
	return nil, nil
}

func (stubReadDataReader) QueryRecords(context.Context, ingestion.PublishedDataModel, string, string, string, int) ([]map[string]any, error) {
	return nil, nil
}

func (stubReadDataReader) AggregateRecords(context.Context, ingestion.PublishedDataModel, ingestion.AggregateQuery) (any, error) {
	return nil, nil
}

var _ ports.TransactionManager = panicTransactionManager{}
var _ ports.TenantDataReader = stubReadDataReader{}
