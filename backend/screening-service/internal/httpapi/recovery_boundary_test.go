package httpapi

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSourceRetryRejectsGeneralCredential(t *testing.T) {
	for _, mode := range []string{"disabled", "token"} {
		for _, token := range []string{"", "short", strings.Repeat("p", 32)} {
			r := NewRouter(slog.Default(), nil, RouterConfig{AuthMode: mode, AuthToken: "general", ScreeningOperatorToken: token})
			for _, route := range []struct{ method, path string }{
				{"POST", "/v1/tenants/foreign/screenings/foreign/retry"},
				{"GET", "/v1/tenants/foreign/screenings/foreign"},
				{"GET", "/v1/tenants/foreign/decisions/foreign/screenings"},
			} {
				req := httptest.NewRequest(route.method, route.path, nil)
				req.Header.Set("Authorization", "Bearer general")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != 403 {
					t.Fatalf("%s: %d", mode, w.Code)
				}
			}
		}
	}
}

func TestEveryLegacyOwnerRouteRejectsGeneralCredential(t *testing.T) {
	for _, mode := range []string{"disabled", "token"} {
		for _, secret := range []string{"", "short", strings.Repeat("p", 32)} {
			r := NewRouter(slog.Default(), nil, RouterConfig{AuthMode: mode, AuthToken: "general", ScreeningOperatorToken: secret})
			count := 0
			for _, route := range r.Routes() {
				if !strings.HasPrefix(route.Path, "/v1/") || strings.HasPrefix(route.Path, "/v1/manual-screenings") || strings.HasPrefix(route.Path, "/v1/investigator/") {
					continue
				}
				parts := strings.Split(route.Path, "/")
				for i, part := range parts {
					if strings.HasPrefix(part, ":") {
						parts[i] = "foreign"
					}
				}
				req := httptest.NewRequest(route.Method, strings.Join(parts, "/"), nil)
				req.Header.Set("Authorization", "Bearer general")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != 403 || !strings.Contains(w.Body.String(), "screening_owner_requires_private_operator") {
					t.Fatalf("%s %s %s escaped owner gate: %d %s", mode, route.Method, route.Path, w.Code, w.Body.String())
				}
				count++
			}
			if count < 30 {
				t.Fatalf("incomplete route coverage: %d", count)
			}
			t.Logf("%s: checked %d owner routes", mode, count)
		}
	}
}

func TestOwnerMetadataRequiresPrivateCredentialButManualRoutesStaySeparate(t *testing.T) {
	secret := strings.Repeat("p", 32)
	r := NewRouter(slog.Default(), nil, RouterConfig{AuthMode: "token", AuthToken: "general", ScreeningOperatorToken: secret})
	req := httptest.NewRequest("GET", "/v1/service-info", nil)
	req.Header.Set("Authorization", "Bearer general")
	req.Header.Set("X-Screening-Operator-Token", secret)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("authorized owner blocked", w.Code)
	}
	for _, path := range []string{"/v1/manual-screenings", "/v1/investigator/tenants/t/cases/c/review", "/internal/v1/tenants/t/decision-screenings"} {
		req := httptest.NewRequest("GET", path, nil)
		if strings.HasPrefix(path, "/internal") {
			req = httptest.NewRequest("POST", path, strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer general")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "screening_owner_requires_private_operator") {
			t.Fatal("owner gate leaked into independent route", path)
		}
	}
}

func TestSourceRecoveryCredentialSeparation(t *testing.T) {
	secret := strings.Repeat("p", 32)
	for _, general := range []string{"general", secret} {
		r := gin.New()
		r.Use(screeningOwnerBoundary(secret, general), authMiddleware(AuthConfig{Mode: "token", Token: general}))
		r.POST("/v1/tenants/:tenantId/screenings/:screeningId/retry", func(c *gin.Context) { c.Status(204) })
		for _, tc := range []struct {
			private, bearer string
			want            int
		}{
			{"", "Bearer " + general, 403}, {general, "Bearer " + general, 403}, {secret, "", 401}, {secret, "Bearer " + general, 204},
		} {
			want := tc.want
			if general == secret {
				want = 403
			}
			req := httptest.NewRequest("POST", "/v1/tenants/t/screenings/s/retry", nil)
			req.Header.Set("Authorization", tc.bearer)
			req.Header.Set("X-Screening-Operator-Token", tc.private)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != want {
				t.Fatalf("got %d want %d", w.Code, want)
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("missing no-store")
			}
		}
	}
}
