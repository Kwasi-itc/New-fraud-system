package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestQueueRejectsMalformedDecisionFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := CaseHandler{}
	r.GET("/tenants/:tenantId/case-queue", h.Queue)
	for _, id := range []string{"bad-id", "00000000-0000-0000-0000-000000000000"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/tenants/11111111-1111-4111-8111-111111111111/case-queue?decision_id="+id, nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", id, w.Code, w.Body.String())
		}
	}
}
