package httpapi

import (
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalScreeningRejectsGeneralAndDisabledAuth(t *testing.T) {
	for _, mode := range []string{"token", "disabled"} {
		for _, secret := range []string{"", "short", strings.Repeat("s", 32), "general"} {
			r := NewRouter(slog.Default(), nil, RouterConfig{AuthMode: mode, AuthToken: "general", ScreeningIntakeToken: secret})
			req := httptest.NewRequest("POST", "/internal/v1/tenants/t/decision-screenings", strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer general")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			want := 503
			if len(secret) >= 32 {
				want = 401
			}
			if w.Code != want {
				t.Fatalf("got %d want %d", w.Code, want)
			}
		}
	}
}
func TestInternalScreeningDedicatedCredential(t *testing.T) {
	secret := strings.Repeat("s", 32)
	r := gin.New()
	r.Use(screeningServiceBoundary(secret, "general", "operator"))
	r.POST("/callback", func(c *gin.Context) { c.Status(204) })
	for _, bearer := range []string{"", "Bearer general", "Bearer operator", "Bearer " + secret} {
		req := httptest.NewRequest("POST", "/callback", nil)
		req.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 401
		if bearer == "Bearer "+secret {
			want = 204
		}
		if w.Code != want {
			t.Fatal(w.Code, want)
		}
	}
}
