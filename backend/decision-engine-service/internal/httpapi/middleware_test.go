package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestContextMiddlewareSetsRequestIDHeader(t *testing.T) {
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
	if !strings.Contains(logs.String(), "request_id=") {
		t.Fatalf("expected request id in logs, got %s", logs.String())
	}
}

func TestRequestContextMiddlewarePreservesIncomingRequestID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	router := gin.New()
	router.Use(requestContextMiddleware(logger))
	router.GET("/healthz", func(c *gin.Context) {
		value, _ := c.Get("request_id")
		if value != "incoming-id" {
			t.Fatalf("expected incoming request id, got %v", value)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.Header.Set(requestIDHeader, "incoming-id")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if rec.Header().Get(requestIDHeader) != "incoming-id" {
		t.Fatalf("expected echoed request id header, got %s", rec.Header().Get(requestIDHeader))
	}
}
