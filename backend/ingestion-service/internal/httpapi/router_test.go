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

	router := NewRouter(slog.Default(), nil, RouterConfig{
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
