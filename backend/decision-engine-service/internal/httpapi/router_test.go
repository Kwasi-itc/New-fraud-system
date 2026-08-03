package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRouterHealthz(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, RouterConfig{
		AuthMode:              "disabled",
		AllowedOrigins:        []string{"http://localhost:3000"},
		DataModelServiceURL:   "http://example.com",
		IngestionServiceURL:   "http://example.com",
		TenantDataReadMode:    "ingestion_http",
		HTTPClientTimeout:     time.Second,
		AggregatePushdownMode: "enabled",
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouterExposesRuntimeMetricsEndpoint(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, RouterConfig{
		AuthMode:              "disabled",
		AllowedOrigins:        []string{"http://localhost:3000"},
		DataModelServiceURL:   "http://example.com",
		IngestionServiceURL:   "http://example.com",
		TenantDataReadMode:    "ingestion_http",
		HTTPClientTimeout:     time.Second,
		AggregatePushdownMode: "enabled",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/runtime-metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouterRequiresAuthTokenWhenConfigured(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, RouterConfig{
		AuthMode:              "token",
		AuthToken:             "secret-token",
		AllowedOrigins:        []string{"http://localhost:3000"},
		DataModelServiceURL:   "http://example.com",
		IngestionServiceURL:   "http://example.com",
		TenantDataReadMode:    "direct_db",
		HTTPClientTimeout:     time.Second,
		AggregatePushdownMode: "enabled",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/runtime-metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}
