package httpapi

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

var executionRoutes = []struct{ method, path, request string }{
	{"GET", "/v1/tenants/:tenantId/decisions/:decisionId/screening-executions", "/v1/tenants/t/decisions/d/screening-executions"},
	{"GET", "/v1/tenants/:tenantId/screening-executions/:executionId", "/v1/tenants/t/screening-executions/e"},
	{"POST", "/v1/tenants/:tenantId/screening-executions/:executionId/status", "/v1/tenants/t/screening-executions/e/status"},
	{"POST", "/v1/tenants/:tenantId/screening-executions/:executionId/retry", "/v1/tenants/t/screening-executions/e/retry"},
}

func TestLegacyExecutionRoutesRejectGeneralCredential(t *testing.T) {
	for _, mode := range []string{"disabled", "token"} {
		for _, secret := range []string{"", "short", strings.Repeat("p", 32)} {
			router := mustScreeningRouter(t, slog.Default(), nil, RouterConfig{AuthMode: mode, AuthToken: "general", ScreeningAuthoringToken: secret})
			for _, route := range executionRoutes {
				req := httptest.NewRequest(route.method, route.request, nil)
				req.Header.Set("Authorization", "Bearer general")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != 403 {
					t.Fatalf("%s %s: %d", mode, route.path, w.Code)
				}
				if strings.Contains(w.Body.String(), secret) && len(secret) > 0 {
					t.Fatal("secret leaked")
				}
			}
		}
	}
}

func TestExecutionOperatorRequiresBothConfiguredCredentials(t *testing.T) {
	secret := strings.Repeat("p", 32)
	router := gin.New()
	router.Use(screeningExecutionBoundary(secret), authMiddleware(AuthConfig{Mode: "token", Token: "general"}))
	for _, route := range executionRoutes {
		router.Handle(route.method, route.path, func(c *gin.Context) { c.Status(204) })
	}
	router.POST("/internal/screening-status-updates", func(c *gin.Context) { c.Status(204) })
	for _, route := range executionRoutes {
		for _, tc := range []struct {
			private, bearer string
			want            int
		}{
			{"", "Bearer general", 403}, {"general", "Bearer general", 403}, {secret, "", 401}, {secret, "Bearer general", 204},
		} {
			req := httptest.NewRequest(route.method, route.request, nil)
			req.Header.Set("Authorization", tc.bearer)
			req.Header.Set("X-Screening-Authoring-Token", tc.private)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("%s: got %d want %d", route.path, w.Code, tc.want)
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("missing no-store")
			}
		}
	}
	req := httptest.NewRequest("POST", "/internal/screening-status-updates", nil)
	req.Header.Set("Authorization", "Bearer general")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatal("callback unexpectedly requires operator credential")
	}
}
