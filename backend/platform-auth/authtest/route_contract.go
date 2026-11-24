package authtest

import (
	"encoding/json"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AssertSignedRoutes checks the actual service route table, including internal
// callbacks, rather than repeating a hand-written subset of protected routes.
func AssertSignedRoutes(t *testing.T, engine *gin.Engine) {
	t.Helper()
	public := func(path string) bool {
		return path == "/healthz" || path == "/readyz" || path == "/docs" || strings.HasPrefix(path, "/docs/") || path == "/redoc" || path == "/openapi.json" || path == "/openapi.yaml"
	}
	tested := 0
	for _, route := range engine.Routes() {
		if public(route.Path) {
			continue
		}
		parts := strings.Split(route.Path, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, ":") {
				parts[i] = "11111111-1111-4111-8111-111111111111"
			} else if strings.HasPrefix(part, "*") {
				parts[i] = ""
			}
		}
		for _, credential := range []string{"", "Bearer retired-static-token", "Basic retired-static-token"} {
			req := httptest.NewRequest(route.Method, strings.Join(parts, "/"), nil)
			req.Header.Set("Authorization", credential)
			for _, header := range []string{"X-Actor-ID", "X-Screening-User", "X-Screening-Session", "X-Screening-Version", "X-Screening-Authoring-Token", "X-Screening-Operator-Token", "X-Workflow-Authoring-Token"} {
				req.Header.Set(header, "forged-privilege")
			}
			req.Header.Set("X-Tenant-ID", "11111111-1111-4111-8111-111111111111")
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Errorf("%s %s accepted unverified identity: %d %s", route.Method, route.Path, w.Code, w.Body.String())
			}
		}
		tested++
	}
	if tested == 0 {
		t.Fatal("no protected routes were exercised")
	}
}

// ObserveRoutePolicies writes an opt-in artifact from the actual registration
// registry. The environment switch exists only in this testing package.
func ObserveRoutePolicies(t testing.TB, service string) func([]auth.RoutePolicy) {
	t.Helper()
	return func(routes []auth.RoutePolicy) {
		if len(routes) == 0 {
			t.Error("empty policy inventory")
			return
		}
		directory := os.Getenv("AUTH_ROUTE_INVENTORY_DIR")
		if directory == "" {
			return
		}
		if filepath.Base(service) != service {
			t.Fatal("invalid inventory service")
		}
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(routes, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, service+".json"), append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
