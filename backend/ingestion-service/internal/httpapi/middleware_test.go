package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"log/slog"
)

func TestRequestContextMiddlewareSetsRequestLoggerAndRequestID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	router := gin.New()
	router.Use(requestContextMiddleware(logger))
	router.GET("/healthz", func(c *gin.Context) {
		value, ok := c.Get("request_id")
		if !ok || value == "" {
			t.Fatal("expected request_id in context")
		}
		loggerValue, ok := c.Get("logger")
		if !ok || loggerValue == nil {
			t.Fatal("expected request-scoped logger in context")
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Header().Get(requestIDHeader) == "" {
		t.Fatal("expected request id header")
	}
	if logs.String() == "" {
		t.Fatal("expected request log output")
	}
}
