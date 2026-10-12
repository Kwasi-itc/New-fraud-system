package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkflowConfigurationRequiresPrivateCredential(t *testing.T) {
	for _, token := range []string{"", strings.Repeat("x", 32)} {
		r := gin.New()
		r.Use(workflowConfigurationBoundary(token))
		r.POST("/v1/tenants/:tenantId/scenarios/:scenarioId/workflow-rules", func(c *gin.Context) { c.Status(204) })
		for _, header := range []string{"", "public-service-token", strings.Repeat("x", 32)} {
			req := httptest.NewRequest(http.MethodPost, "/v1/tenants/t/scenarios/s/workflow-rules", nil)
			req.Header.Set("X-Workflow-Authoring-Token", header)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			want := 403
			if token != "" && header == token {
				want = 204
			}
			if w.Code != want {
				t.Fatalf("got %d want %d", w.Code, want)
			}
		}
	}
}
