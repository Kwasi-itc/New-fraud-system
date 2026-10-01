package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestAnalyticsRejectsMalformedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := CaseHandler{}
	r.GET("/tenants/:tenantId/case-analytics", h.Analytics)
	for _, path := range []string{
		"/tenants/not-uuid/case-analytics",
		"/tenants/11111111-1111-4111-8111-111111111111/case-analytics",
		"/tenants/11111111-1111-4111-8111-111111111111/case-analytics?from=2026-01-01T00:00:00Z&to=invalid",
		"/tenants/11111111-1111-4111-8111-111111111111/case-analytics?from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z&inbox_id=invalid",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
