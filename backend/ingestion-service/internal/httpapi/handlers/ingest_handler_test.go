package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

func TestIngestHandlerRejectsNonObjectPayload(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewIngestHandler(service.NewIngestService(
		stubDataModelReader{},
		stubTransactionManager{},
		stubIDGenerator{},
		stubClock{},
	))
	router := gin.New()
	router.POST("/v1/tenants/:tenantId/ingest/:objectType", handler.PostIngest)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/tenants/11111111-1111-1111-1111-111111111111/ingest/transactions",
		bytes.NewBufferString(`[]`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

type stubDataModelReader struct{}

func (stubDataModelReader) GetPublishedDataModel(context.Context, uuid.UUID) (ingestion.PublishedDataModel, error) {
	return ingestion.PublishedDataModel{
		RevisionID:          "rev",
		TenantStatus:        "active",
		Writable:            true,
		RecordLookupField:   "object_id",
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {
				Fields: map[string]ingestion.FieldSchema{
					"status": {Name: "status", DataType: "string", Nullable: false},
				},
			},
		},
	}, nil
}

type stubTransactionManager struct{}

func (stubTransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	return fn(stubMutationStore{})
}

type stubMutationStore struct{}

func (stubMutationStore) Audits() ports.IngestionAuditRepository    { return stubAuditRepo{} }
func (stubMutationStore) Idempotency() ports.IdempotencyRepository  { return stubIdempotencyRepo{} }
func (stubMutationStore) OutboxEvents() ports.OutboxEventRepository { return stubOutboxRepo{} }
func (stubMutationStore) UploadLogs() ports.UploadLogRepository     { return stubUploadLogRepo{} }
func (stubMutationStore) TenantWriter() ports.TenantDataWriter      { return stubTenantWriter{} }
func (stubMutationStore) TenantReader() ports.TenantDataReader      { return stubTenantReader{} }

type stubAuditRepo struct{}

func (stubAuditRepo) Create(context.Context, ingestion.IngestionAudit) error { return nil }

type stubIdempotencyRepo struct{}

func (stubIdempotencyRepo) Get(context.Context, uuid.UUID, string) (*ingestion.IdempotencyKey, error) {
	return nil, nil
}
func (stubIdempotencyRepo) Create(context.Context, ingestion.IdempotencyKey) error { return nil }

type stubOutboxRepo struct{}

func (stubOutboxRepo) Create(context.Context, ingestion.OutboxEvent) error { return nil }

type stubUploadLogRepo struct{}

func (stubUploadLogRepo) Create(context.Context, ingestion.UploadLog) error { return nil }
func (stubUploadLogRepo) ListByTenantAndObjectType(context.Context, uuid.UUID, string) ([]ingestion.UploadLog, error) {
	return nil, nil
}
func (stubUploadLogRepo) GetByID(context.Context, uuid.UUID) (ingestion.UploadLog, error) {
	return ingestion.UploadLog{}, nil
}
func (stubUploadLogRepo) Update(context.Context, ingestion.UploadLog) error { return nil }
func (stubUploadLogRepo) ClaimNextUploaded(context.Context, time.Time) (*ingestion.UploadLog, error) {
	return nil, nil
}

type stubTenantWriter struct{}

func (stubTenantWriter) UpsertRecord(context.Context, ingestion.PublishedDataModel, string, map[string]any, ingestion.Mode, time.Time) (string, error) {
	return "created", nil
}

type stubTenantReader struct{}

func (stubTenantReader) GetRecord(context.Context, ingestion.PublishedDataModel, string, string) (map[string]any, error) {
	return map[string]any{"object_id": "obj-1"}, nil
}

func (stubTenantReader) ListRecords(context.Context, ingestion.PublishedDataModel, string, int) ([]map[string]any, error) {
	return []map[string]any{{"object_id": "obj-1"}}, nil
}

func (stubTenantReader) QueryRecords(context.Context, ingestion.PublishedDataModel, string, string, string, int) ([]map[string]any, error) {
	return []map[string]any{{"object_id": "obj-1"}}, nil
}

func (stubTenantReader) AggregateRecords(context.Context, ingestion.PublishedDataModel, ingestion.AggregateQuery) (any, error) {
	return float64(1), nil
}

type stubIDGenerator struct{}

func (stubIDGenerator) New() uuid.UUID { return uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa") }

type stubClock struct{}

func (stubClock) Now() time.Time { return time.Unix(0, 0).UTC() }
