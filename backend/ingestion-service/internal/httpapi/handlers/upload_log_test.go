package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

func TestUploadLogHandlerRejectsWhenWriteConcurrencyLimitReached(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	writeLimiter := make(chan struct{}, 1)
	handler := NewUploadLogHandler(service.UploadLogService{}, UploadLogHandlerConfig{
		WritePathLimiter: writeLimiter,
	})
	writeLimiter <- struct{}{}
	defer func() { <-writeLimiter }()

	router := gin.New()
	router.POST("/v1/tenants/:tenantId/ingest/:objectType/csv", handler.CreateCSV)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/tenants/11111111-1111-1111-1111-111111111111/ingest/transactions/csv",
		nil,
	)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}
