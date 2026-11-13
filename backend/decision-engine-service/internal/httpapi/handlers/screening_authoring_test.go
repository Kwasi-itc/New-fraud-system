package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScreeningAuthoringRejectsInvalidInputBeforePersistence(t *testing.T) {
	id := uuid.NewString()
	for _, tc := range []struct {
		body, contentType string
		want              int
	}{
		{`{}`, "application/json", 400},
		{`{"unknown":true}`, "application/json", 400},
		{strings.Repeat(" ", 8193), "application/json", 400},
		{`{}`, "text/plain", 415},
	} {
		router := gin.New()
		h := ScreeningAuthoringHandler{}
		router.PUT("/tenants/:tenantId/scenarios/:scenarioId/configs/:configId", h.Handle)
		req := httptest.NewRequest("PUT", "/tenants/"+uuid.NewString()+"/scenarios/"+uuid.NewString()+"/configs/"+id, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.contentType)
		req.Header.Set("X-Screening-User", uuid.NewString())
		req.Header.Set("X-Screening-Session", uuid.NewString())
		req.Header.Set("X-Screening-Version", "1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("got %d want %d", w.Code, tc.want)
		}
	}
}
