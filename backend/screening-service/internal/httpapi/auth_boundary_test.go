package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Exercise the boundary directly as well as startup validation: routers can be
// constructed programmatically without LoadConfig.
func TestServiceAuthFailsClosed(t *testing.T) {
	tests := []struct {
		name, mode, token, authorization string
		status                           int
	}{
		{"missing mode", "", "", "", 500},
		{"unknown mode", "tokne", "test-secret", "Bearer test-secret", 500},
		{"empty configured token", "token", "", "Bearer ", 500},
		{"whitespace configured token", "token", "   ", "Bearer    ", 500},
		{"missing credential", "token", "test-secret", "", 401},
		{"wrong credential", "token", "test-secret", "Bearer wrong-secret", 401},
		{"wrong scheme", "token", "test-secret", "Basic test-secret", 401},
		{"extra suffix", "token", "test-secret", "Bearer test-secret-more", 401},
		{"valid credential", "token", "test-secret", "Bearer test-secret", 204},
		{"explicit development opt out", "disabled", "", "", 204},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached := false
			router := gin.New()
			router.Use(authMiddleware(AuthConfig{Mode: tt.mode, Token: tt.token}))
			router.POST("/v1/protected", func(c *gin.Context) { reached = true; c.Status(204) })
			req := httptest.NewRequest(http.MethodPost, "/v1/protected", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.status || reached != (tt.status == 204) {
				t.Fatalf("status=%d handlerReached=%v, want status=%d", rec.Code, reached, tt.status)
			}
			if strings.Contains(rec.Body.String(), "test-secret") {
				t.Fatal("credential leaked into response")
			}
		})
	}
}
