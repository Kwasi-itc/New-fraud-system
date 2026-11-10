package httpapi

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRegisteredIntegrationPreservesReceiptEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := uuid.New()
	const limit = (1 << 20) + 4096
	for _, tc := range []struct {
		name       string
		size, want int
	}{{"small", 128, 204}, {"largest_envelope", limit, 204}, {"too_large", limit + 1, 400}} {
		t.Run(tc.name, func(t *testing.T) {
			size := tc.size
			prefix := `{"tenant_id":"` + tenant.String() + `","padding":"`
			suffix := `"}`
			body := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
			router := gin.New()
			router.Use(authMiddleware(AuthConfig{Mode: "token", Token: "private-test-token", TenantAllowed: func(_ context.Context, next uuid.UUID) (bool, error) { return next == tenant, nil }}))
			called := false
			router.POST("/integration", func(c *gin.Context) {
				called = true
				if e := access.Integration(c.Request.Context(), tenant); e != nil {
					t.Error(e)
				}
				raw, e := io.ReadAll(c.Request.Body)
				if e != nil || string(raw) != body {
					t.Error("integration authentication changed the original receipt payload")
				}
				c.Status(204)
			})
			req := httptest.NewRequest("POST", "/integration", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer private-test-token")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want || called != (tc.want == 204) {
				t.Fatalf("status=%d handler_called=%v", w.Code, called)
			}
		})
	}
}
