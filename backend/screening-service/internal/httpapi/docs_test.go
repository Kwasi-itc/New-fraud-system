package httpapi

import (
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func TestDocsRoutesServeOpenAPISpecAndDocsPage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	registerDocsRoutes(authtest.DomainRouter(router))

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

func TestControlledManualOpenAPIHasUniqueKeysAndSessionBoundRoutes(t *testing.T) {
	var spec struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatal("invalid or duplicate OpenAPI mapping", err)
	}
	if spec.OpenAPI != "3.0.3" {
		t.Fatal("OpenAPI version missing")
	}
	for _, path := range []string{"/v1/manual-screenings/capabilities", "/v1/manual-screenings", "/v1/manual-screenings/{id}", "/v1/manual-screenings/{id}/matches/{matchId}/enrich", "/v1/investigator/tenants/{tenantId}/cases/{caseId}/matches/{matchId}/enrich"} {
		operation, ok := spec.Paths[path]
		if !ok {
			t.Fatal("manual route undocumented", path)
		}
		for method, value := range operation {
			if method != "get" && method != "post" {
				continue
			}
			data, ok := value.(map[string]any)
			if !ok || data["security"] == nil {
				t.Fatal("session security missing", path, method)
			}
		}
	}
}
