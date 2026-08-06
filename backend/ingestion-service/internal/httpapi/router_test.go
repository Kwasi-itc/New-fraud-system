package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRouterHealthz(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRouterHandlesCORSPreflight(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
	})

	req := httptest.NewRequest(http.MethodOptions, "/v1/tenants/test/ingest/accounts/csv", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("expected allow origin header, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestRouterExposesReadMetricsEndpoint(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
		OverloadThresholds: OverloadThresholds{
			DBPoolSaturationPct:    80,
			RequestQueueDepth:      8,
			ServiceCPUPercent:      85,
			UpstreamTimeoutRatePct: 5,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/read-metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "\"thresholds\"") {
		t.Fatalf("expected thresholds in response body, got %s", body)
	}
	if !strings.Contains(body, "\"db_pool_saturation_pct\":80") {
		t.Fatalf("expected db pool saturation threshold in response body, got %s", body)
	}
}

func TestRouterExposesDeferredIngestMetricsEndpoint(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, nil, RouterConfig{
		AuthMode:            "disabled",
		AllowedOrigins:      []string{"http://localhost:3000"},
		DataModelServiceURL: "http://example.com",
		HTTPClientTimeout:   time.Second,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/deferred-ingest-metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"deferred_ingest_metrics\"") {
		t.Fatalf("expected deferred_ingest_metrics in response body, got %s", rec.Body.String())
	}
}
