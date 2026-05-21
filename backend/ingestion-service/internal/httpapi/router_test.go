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
