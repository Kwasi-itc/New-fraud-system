package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

func TestDeferredIngestHandlerReturnsExecution(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	handler := NewDeferredIngestHandler(service.NewDeferredIngestService(
		stubDeferredIngestStatusRepo{},
		service.IngestService{},
		nil,
		stubIDGenerator{},
		stubClock{},
		3,
		nil,
	))
	router := gin.New()
	router.GET("/v1/deferred-ingests/:deferredIngestId", handler.Get)

	req := httptest.NewRequest(http.MethodGet, "/v1/deferred-ingests/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "" || !strings.Contains(body, "\"deferred_ingest\"") || !strings.Contains(body, "\"status\":\"queued\"") {
		t.Fatalf("unexpected response body: %s", body)
	}
}

type stubDeferredIngestStatusRepo struct{}

func (stubDeferredIngestStatusRepo) Create(context.Context, ingestion.DeferredIngest) error {
	return nil
}

func (stubDeferredIngestStatusRepo) GetByID(context.Context, uuid.UUID) (ingestion.DeferredIngest, error) {
	requestedAt := time.Unix(0, 0).UTC()
	return ingestion.DeferredIngest{
		ID:           "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		TenantID:     "11111111-1111-1111-1111-111111111111",
		ObjectType:   "transactions",
		Mode:         ingestion.ModeCreate,
		Status:       ingestion.DeferredIngestStatusQueued,
		AttemptCount: 1,
		RequestedAt:  requestedAt,
	}, nil
}

func (stubDeferredIngestStatusRepo) Update(context.Context, ingestion.DeferredIngest) error {
	return nil
}

func (stubDeferredIngestStatusRepo) StartAttempt(context.Context, uuid.UUID, time.Time) (ingestion.DeferredIngest, error) {
	return ingestion.DeferredIngest{}, nil
}

func (stubDeferredIngestStatusRepo) MetricsSnapshot(context.Context, time.Time) (ingestion.DeferredIngestMetrics, error) {
	return ingestion.DeferredIngestMetrics{}, nil
}
