package platform

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/google/uuid"
)

func TestAuthRoutePoliciesDenyRetiredCredentialsAndForeignTenants(t *testing.T) {
	tenant, other := uuid.New(), uuid.New()
	verifier, human := authtest.HumanIdentity(t, tenant, "roles:read")
	var policies []auth.RoutePolicy
	observe := authtest.ObserveRoutePolicies(t, "auth-service")
	api := (Server{Verifier: verifier, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PolicyObserver: func(routes []auth.RoutePolicy) { policies = routes; observe(routes) }}).Handler()
	parameter := regexp.MustCompile(`\{[^}]+\}`)
	protected, bootstrap, public, authenticated := 0, 0, 0, 0
	for _, route := range policies {
		if err := route.Policy.Validate(); err != nil {
			t.Fatal(err)
		}
		if route.Public {
			public++
			continue
		}
		path := strings.ReplaceAll(route.Path, "{tenantId}", other.String())
		path = parameter.ReplaceAllString(path, uuid.NewString())
		for _, credential := range []string{"", "retired-shared-secret", human} {
			req := httptest.NewRequest(route.Method, path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			if credential != "" {
				req.Header.Set("Authorization", "Bearer "+credential)
			}
			req.Header.Set("X-Actor-ID", uuid.NewString())
			req.Header.Set("X-Workflow-Authoring-Token", "retired-shared-secret")
			response := httptest.NewRecorder()
			api.ServeHTTP(response, req)
			want := 401
			if credential == human && route.AuthenticatedOnly {
				want = 503
			} else if credential == human && !route.ControlPlane {
				want = 403
			}
			if response.Code != want {
				t.Fatalf("%s %s human_credential=%t status=%d want=%d", route.Method, route.Path, credential == human, response.Code, want)
			}
		}
		if route.ControlPlane {
			bootstrap++
		} else {
			protected++
			if route.AuthenticatedOnly {
				authenticated++
			}
		}
	}
	if public != 3 || bootstrap == 0 || protected == 0 || authenticated == 0 {
		t.Fatalf("incomplete auth inventory: public=%d bootstrap=%d human=%d authenticated_only=%d", public, bootstrap, protected, authenticated)
	}
}
