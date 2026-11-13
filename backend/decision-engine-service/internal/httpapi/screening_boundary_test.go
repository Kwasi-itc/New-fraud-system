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

func TestScreeningConfigurationRejectsLegacyCredential(t *testing.T) {
	for _, mode := range []string{"disabled", "token"} {
		router := NewRouter(slog.Default(), nil, RouterConfig{
			AuthMode: mode, AuthToken: "legacy-token",
			DataModelServiceURL: "http://127.0.0.1:1",
			IngestionServiceURL: "http://127.0.0.1:1",
			TenantDataReadMode:  "ingestion_http", HTTPClientTimeout: time.Second,
		})
		for _, route := range []struct{ method, path string }{
			{"GET", ""}, {"POST", ""}, {"GET", "/config"}, {"PUT", "/config"}, {"DELETE", "/config"},
		} {
			req := httptest.NewRequest(route.method, "/v1/tenants/other-tenant/scenarios/scenario/screening-configs"+route.path, nil)
			req.Header.Set("Authorization", "Bearer legacy-token")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s: got %d, want 403 before handler access", mode, route.method, w.Code)
			}
		}
	}
}

func TestScreeningOperatorBoundaryRequiresSeparateCredential(t *testing.T) {
	for _, token := range []string{"", "short", strings.Repeat("s", 32)} {
		router := gin.New()
		router.Use(screeningConfigurationBoundary(token))
		router.Use(authMiddleware(AuthConfig{Mode: "token", Token: "legacy-token"}))
		router.PUT("/v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId", func(c *gin.Context) { c.Status(204) })
		router.GET("/v1/tenants/:tenantId/decisions/:decisionId/screening-executions", func(c *gin.Context) { c.Status(204) })
		for _, supplied := range []string{"", "legacy-token", strings.Repeat("s", 32)} {
			for _, bearer := range []string{"", "Bearer legacy-token"} {
				req := httptest.NewRequest("PUT", "/v1/tenants/t/scenarios/s/screening-configs/c", nil)
				req.Header.Set("Authorization", bearer)
				req.Header.Set("X-Screening-Authoring-Token", supplied)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				want := 403
				if len(token) >= 32 && supplied == token {
					want = 401
					if bearer != "" {
						want = 204
					}
				}
				if w.Code != want {
					t.Fatalf("got %d want %d", w.Code, want)
				}
			}
		}
		req := httptest.NewRequest("GET", "/v1/tenants/t/decisions/d/screening-executions", nil)
		req.Header.Set("Authorization", "Bearer legacy-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 204 {
			t.Fatal("configuration boundary changed execution polling")
		}
	}
}
