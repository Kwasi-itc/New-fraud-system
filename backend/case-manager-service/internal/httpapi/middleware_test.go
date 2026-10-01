package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestServiceAuthenticationFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := uuid.New()
	for _, tt := range []struct {
		name   string
		cfg    AuthConfig
		header string
		want   int
	}{
		{"disabled", AuthConfig{Mode: "disabled"}, "", 401},
		{"empty token", AuthConfig{Mode: "token", TenantIDs: []uuid.UUID{tenant}}, "Bearer ", 401},
		{"no tenants", AuthConfig{Mode: "token", Token: "secret"}, "Bearer secret", 401},
		{"wrong token", AuthConfig{Mode: "token", Token: "secret", TenantIDs: []uuid.UUID{tenant}}, "Bearer attacker", 401},
		{"valid", AuthConfig{Mode: "token", Token: "secret", TenantIDs: []uuid.UUID{tenant}}, "Bearer secret", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(authMiddleware(tt.cfg))
			r.GET("/", func(c *gin.Context) {
				p, ok := access.FromContext(c.Request.Context())
				if !ok || p.Kind != access.Service || access.Actor(c.Request.Context()) != nil {
					t.Fatal("service was treated as a human")
				}
				c.Status(200)
			})
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", tt.header)
			req.Header.Set("X-Actor-ID", "admin")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
}

func TestUserBoundaryRejectsUnverifiedHeaders(t *testing.T) {
	r := gin.New()
	r.Use(userMiddleware(nil))
	r.GET("/", func(c *gin.Context) { t.Fatal("unverified request reached handler") })
	for _, header := range []string{"", "Bearer service-token", "Bearer malformed.jwt.token"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", header)
		req.Header.Set("X-Actor-ID", "admin")
		req.Header.Set("X-Tenant-ID", uuid.NewString())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("unverified request: %d", w.Code)
		}
	}
}
