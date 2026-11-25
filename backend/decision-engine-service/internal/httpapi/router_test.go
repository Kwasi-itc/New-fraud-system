package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestClientScenarioAuthoringRoutesRemainDraftOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var routes []platformauth.RoutePolicy
	NewRouter(slog.Default(), nil, RouterConfig{
		PolicyObserver: func(policies []platformauth.RoutePolicy) { routes = policies },
	})
	byKey := make(map[string]platformauth.Policy, len(routes))
	for _, route := range routes {
		byKey[route.Method+" "+route.Path] = route.Policy
	}

	clientAuthoring := []string{
		"POST /v1/tenants/:tenantId/scenarios",
		"POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations",
		"POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/draft",
		"PUT /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId",
		"POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules",
		"POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/validate",
	}
	for _, key := range clientAuthoring {
		policy, ok := byKey[key]
		if !ok || policy.Capability != "scenarios:write" || policy.PrincipalType != "" {
			t.Errorf("%s must allow capability-authorized clients, got %+v (present=%v)", key, policy, ok)
		}
	}

	humanOnly := []string{
		"DELETE /v1/tenants/:tenantId/scenarios/:scenarioId",
		"PUT /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId",
		"GET /v1/tenants/:tenantId/test-runs/:testRunId/decision-data-by-score",
		"GET /v1/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule",
		"POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/commit",
		"POST /v1/tenants/:tenantId/scenarios/:scenarioId/publications/preparation",
		"POST /v1/tenants/:tenantId/scenarios/:scenarioId/publications",
		"GET /v1/tenants/:tenantId/async-decision-executions/:executionId",
	}
	for _, key := range humanOnly {
		policy, ok := byKey[key]
		if !ok || policy.PrincipalType != platformauth.PrincipalUser {
			t.Errorf("%s must remain human-only, got %+v (present=%v)", key, policy, ok)
		}
		clientStatus := byKey["GET /v1/tenants/:tenantId/client-async-decision-executions/:executionId"]
		if clientStatus.PrincipalType != platformauth.PrincipalClient || clientStatus.Capability != "decisions:evaluate" {
			t.Errorf("client execution status requires a client with decisions:evaluate, got %+v", clientStatus)
		}
	}
}

func TestRouterHealthz(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := NewRouter(slog.Default(), nil, RouterConfig{

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

	verifier, token := authtest.HumanIdentity(t, uuid.New(), "metrics:read")
	router := NewRouter(slog.Default(), nil, RouterConfig{Verifier: verifier,

		AllowedOrigins:        []string{"http://localhost:3000"},
		DataModelServiceURL:   "http://example.com",
		IngestionServiceURL:   "http://example.com",
		TenantDataReadMode:    "ingestion_http",
		HTTPClientTimeout:     time.Second,
		AggregatePushdownMode: "enabled",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/runtime-metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
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
