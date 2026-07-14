package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDocsRoutesServeOpenAPISpecAndDocsPage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	registerDocsRoutes(router)

	specReq := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	specRec := httptest.NewRecorder()
	router.ServeHTTP(specRec, specReq)

	if specRec.Code != http.StatusOK {
		t.Fatalf("expected openapi.yaml 200, got %d", specRec.Code)
	}
	if !strings.Contains(specRec.Body.String(), "openapi: 3.0.3") {
		t.Fatal("expected OpenAPI header in served spec")
	}

	docsReq := httptest.NewRequest(http.MethodGet, "/docs", nil)
	docsRec := httptest.NewRecorder()
	router.ServeHTTP(docsRec, docsReq)

	if docsRec.Code != http.StatusOK {
		t.Fatalf("expected docs 200, got %d", docsRec.Code)
	}
	if !strings.Contains(docsRec.Body.String(), "SwaggerUIBundle") {
		t.Fatal("expected swagger UI bootstrap in docs page")
	}

	redocReq := httptest.NewRequest(http.MethodGet, "/redoc", nil)
	redocRec := httptest.NewRecorder()
	router.ServeHTTP(redocRec, redocReq)

	if redocRec.Code != http.StatusOK {
		t.Fatalf("expected redoc 200, got %d", redocRec.Code)
	}
	if !strings.Contains(redocRec.Body.String(), "redoc.standalone.js") {
		t.Fatal("expected redoc bootstrap in redoc page")
	}
}
