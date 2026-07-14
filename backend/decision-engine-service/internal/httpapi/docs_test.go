package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDocsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerDocsRoutes(router)

	t.Run("openapi", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/yaml") {
			t.Fatalf("content-type = %q, want application/yaml", got)
		}
		if body := rec.Body.String(); !strings.Contains(body, "openapi: 3.0.3") {
			t.Fatalf("body missing OpenAPI marker")
		}
	})

	t.Run("swagger ui", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
			t.Fatalf("content-type = %q, want text/html", got)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "SwaggerUIBundle") {
			t.Fatalf("body missing Swagger UI bundle")
		}
		if !strings.Contains(body, "/openapi.yaml") {
			t.Fatalf("body missing OpenAPI URL")
		}
	})

	t.Run("redoc", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/redoc", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
			t.Fatalf("content-type = %q, want text/html", got)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<redoc") {
			t.Fatalf("body missing redoc tag")
		}
		if !strings.Contains(body, `spec-url="/openapi.yaml"`) {
			t.Fatalf("body missing Redoc spec URL")
		}
	})
}
