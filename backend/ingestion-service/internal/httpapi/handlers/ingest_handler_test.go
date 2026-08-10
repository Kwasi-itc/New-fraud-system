package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
		nil,
		stubIDGenerator{},
		stubClock{},
	), IngestHandlerConfig{})
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

func TestAggregateHandlerRejectsWhenConcurrencyLimitReached(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewIngestHandler(service.NewIngestService(
		stubDataModelReader{},
		stubTransactionManager{},
		nil,
		stubIDGenerator{},
		stubClock{},
	), IngestHandlerConfig{
		AggregateQueryConcurrencyLimit: 1,
		AggregateQueryTimeout:          time.Second,
	})
	handler.aggregateLimiter <- struct{}{}
	defer func() { <-handler.aggregateLimiter }()

	router := gin.New()
	router.POST("/v1/tenants/:tenantId/query/aggregate", handler.AggregateRecords)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/tenants/11111111-1111-1111-1111-111111111111/query/aggregate",
		bytes.NewBufferString(`{"object_type":"transactions","aggregate":"count","field":"status"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIngestHandlerRejectsWhenWriteConcurrencyLimitReached(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	writeLimiter := make(chan struct{}, 1)
	handler := NewIngestHandler(service.NewIngestService(
		stubDataModelReader{},
		stubTransactionManager{},
		nil,
		stubIDGenerator{},
		stubClock{},
	), IngestHandlerConfig{
		WritePathLimiter: writeLimiter,
	})
	writeLimiter <- struct{}{}
	defer func() { <-writeLimiter }()

	router := gin.New()
	router.POST("/v1/tenants/:tenantId/ingest/:objectType", handler.PostIngest)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/tenants/11111111-1111-1111-1111-111111111111/ingest/transactions",
		bytes.NewBufferString(`{"status":"ok"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIngestHandlerDefersWhenWriteConcurrencyLimitReachedAndAsyncFallbackEnabled(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	writeLimiter := make(chan struct{}, 1)
	handler := NewIngestHandler(service.NewIngestService(
		stubDataModelReader{},
		stubTransactionManager{},
		nil,
		stubIDGenerator{},
		stubClock{},
	), IngestHandlerConfig{
		WritePathLimiter:      writeLimiter,
		WritePathOverloadMode: "defer_async",
		DeferredIngestService: service.NewDeferredIngestService(
			stubDeferredIngestRepo{},
			service.NewIngestService(
				stubDataModelReader{},
				stubTransactionManager{},
				nil,
				stubIDGenerator{},
				stubClock{},
			),
			stubTransactionManager{},
			stubIDGenerator{},
			stubClock{},
			3,
			stubDeferredIngestEnqueuer{},
		),
	})
	writeLimiter <- struct{}{}
	defer func() { <-writeLimiter }()

	router := gin.New()
	router.POST("/v1/tenants/:tenantId/ingest/:objectType", handler.PostIngest)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/tenants/11111111-1111-1111-1111-111111111111/ingest/transactions",
		bytes.NewBufferString(`{"status":"ok","object_id":"txn-1"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["deferred_ingest"]["status"] != string(ingestion.DeferredIngestStatusQueued) {
		t.Fatalf("expected queued deferred ingest, got %v", body["deferred_ingest"]["status"])
	}
}

func TestListRecordsUsesDefaultReadLimit(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	reader := &capturingTenantReader{}
	handler := NewIngestHandler(service.NewIngestService(
		stubDataModelReader{},
		capturingTransactionManager{reader: reader},
		nil,
		stubIDGenerator{},
		stubClock{},
	), IngestHandlerConfig{})
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/records/:objectType", handler.ListRecords)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/tenants/11111111-1111-1111-1111-111111111111/records/transactions",
		nil,
	)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if reader.lastListLimit != defaultRecordReadLimit {
		t.Fatalf("list limit = %d, want %d", reader.lastListLimit, defaultRecordReadLimit)
	}
}

func TestQueryRecordsUsesDefaultReadLimit(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	reader := &capturingTenantReader{}
	handler := NewIngestHandler(service.NewIngestService(
		stubDataModelReader{},
		capturingTransactionManager{reader: reader},
		nil,
		stubIDGenerator{},
		stubClock{},
	), IngestHandlerConfig{})
	router := gin.New()
	router.GET("/v1/tenants/:tenantId/records/:objectType/search", handler.QueryRecords)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/tenants/11111111-1111-1111-1111-111111111111/records/transactions/search?field=status&value=ok",
		nil,
	)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if reader.lastQueryLimit != defaultRecordReadLimit {
		t.Fatalf("query limit = %d, want %d", reader.lastQueryLimit, defaultRecordReadLimit)
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

type capturingTransactionManager struct {
	reader ports.TenantDataReader
}

func (s capturingTransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	return fn(capturingMutationStore{reader: s.reader})
}

type stubMutationStore struct{}

func (stubMutationStore) Audits() ports.IngestionAuditRepository    { return stubAuditRepo{} }
func (stubMutationStore) Idempotency() ports.IdempotencyRepository  { return stubIdempotencyRepo{} }
func (stubMutationStore) OutboxEvents() ports.OutboxEventRepository { return stubOutboxRepo{} }
func (stubMutationStore) UploadLogs() ports.UploadLogRepository     { return stubUploadLogRepo{} }
func (stubMutationStore) DeferredIngests() ports.DeferredIngestRepository {
	return stubDeferredIngestRepo{}
}
func (stubMutationStore) TenantWriter() ports.TenantDataWriter { return stubTenantWriter{} }
func (stubMutationStore) TenantReader() ports.TenantDataReader { return stubTenantReader{} }
func (stubMutationStore) RawTx() pgx.Tx                        { return nil }

type capturingMutationStore struct {
	reader ports.TenantDataReader
}

func (capturingMutationStore) Audits() ports.IngestionAuditRepository    { return stubAuditRepo{} }
func (capturingMutationStore) Idempotency() ports.IdempotencyRepository  { return stubIdempotencyRepo{} }
func (capturingMutationStore) OutboxEvents() ports.OutboxEventRepository { return stubOutboxRepo{} }
func (capturingMutationStore) UploadLogs() ports.UploadLogRepository     { return stubUploadLogRepo{} }
func (capturingMutationStore) DeferredIngests() ports.DeferredIngestRepository {
	return stubDeferredIngestRepo{}
}
func (capturingMutationStore) TenantWriter() ports.TenantDataWriter { return stubTenantWriter{} }
func (s capturingMutationStore) TenantReader() ports.TenantDataReader {
	return s.reader
}
func (capturingMutationStore) RawTx() pgx.Tx { return nil }

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
func (stubUploadLogRepo) StartAttempt(context.Context, uuid.UUID, time.Time) (ingestion.UploadLog, error) {
	return ingestion.UploadLog{}, nil
}

type stubDeferredIngestRepo struct{}

func (stubDeferredIngestRepo) Create(context.Context, ingestion.DeferredIngest) error { return nil }
func (stubDeferredIngestRepo) GetByID(context.Context, uuid.UUID) (ingestion.DeferredIngest, error) {
	return ingestion.DeferredIngest{}, nil
}
func (stubDeferredIngestRepo) Update(context.Context, ingestion.DeferredIngest) error { return nil }
func (stubDeferredIngestRepo) StartAttempt(context.Context, uuid.UUID, time.Time) (ingestion.DeferredIngest, error) {
	return ingestion.DeferredIngest{}, nil
}
func (stubDeferredIngestRepo) MetricsSnapshot(context.Context, time.Time) (ingestion.DeferredIngestMetrics, error) {
	return ingestion.DeferredIngestMetrics{}, nil
}

type stubDeferredIngestEnqueuer struct{}

func (stubDeferredIngestEnqueuer) Enqueue(context.Context, uuid.UUID, *time.Time) error { return nil }
func (stubDeferredIngestEnqueuer) EnqueueTx(context.Context, pgx.Tx, uuid.UUID, *time.Time) error {
	return nil
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

type capturingTenantReader struct {
	lastListLimit  int
	lastQueryLimit int
}

func (capturingTenantReader) GetRecord(context.Context, ingestion.PublishedDataModel, string, string) (map[string]any, error) {
	return map[string]any{"object_id": "obj-1"}, nil
}

func (r *capturingTenantReader) ListRecords(_ context.Context, _ ingestion.PublishedDataModel, _ string, limit int) ([]map[string]any, error) {
	r.lastListLimit = limit
	return []map[string]any{{"object_id": "obj-1"}}, nil
}

func (r *capturingTenantReader) QueryRecords(_ context.Context, _ ingestion.PublishedDataModel, _ string, _ string, _ string, limit int) ([]map[string]any, error) {
	r.lastQueryLimit = limit
	return []map[string]any{{"object_id": "obj-1"}}, nil
}

func (capturingTenantReader) AggregateRecords(context.Context, ingestion.PublishedDataModel, ingestion.AggregateQuery) (any, error) {
	return float64(1), nil
}

type stubIDGenerator struct{}

func (stubIDGenerator) New() uuid.UUID { return uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa") }

type stubClock struct{}

func (stubClock) Now() time.Time { return time.Unix(0, 0).UTC() }
