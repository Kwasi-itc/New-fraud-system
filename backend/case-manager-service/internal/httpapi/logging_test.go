package httpapi

import (
	"bytes"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogsCorrelateAndDoNotExposeInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, panicRequest := range []bool{false, true} {
		var out bytes.Buffer
		r := gin.New()
		r.Use(requestContextMiddleware(observability.New(&out, "api", "debug")), recoverRequest())
		r.POST("/cases/:caseId", func(c *gin.Context) {
			if panicRequest {
				panic("SECRET-PANIC")
			}
			c.Status(403)
		})
		r.POST("/healthz", func(c *gin.Context) { c.Status(200) })
		id := uuid.NewString()
		req := httptest.NewRequest("POST", "/cases/"+id+"?name=SECRET-QUERY", strings.NewReader("SECRET-BODY"))
		req.Header.Set("Authorization", "Bearer SECRET-TOKEN")
		req.Header.Set("Cookie", "session=SECRET-COOKIE")
		req.Header.Set("X-Request-ID", "SECRET-UNTRUSTED-ID")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if _, err := uuid.Parse(w.Header().Get("X-Request-ID")); err != nil {
			t.Fatal("missing request ID", err)
		}
		if panicRequest && w.Code != 500 {
			t.Fatal("panic not recovered", w.Code)
		}
		if strings.Contains(out.String(), "SECRET-") {
			t.Fatal("request content leaked", out.String())
		}
		found := false
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["event"] == "http.request.completed" {
				found = true
				if entry["request_id"] != w.Header().Get("X-Request-ID") || entry["route"] != "/cases/:caseId" || entry["case_id"] != id {
					t.Fatal("missing correlation", entry)
				}
				want := "WARN"
				if panicRequest {
					want = "ERROR"
				}
				if entry["level"] != want {
					t.Fatal("incorrect severity", entry)
				}
			}
		}
		if !found {
			t.Fatal("missing completion")
		}
	}
}

func TestHealthAndUnknownPathsAvoidNoiseAndLeakage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var out bytes.Buffer
	r := gin.New()
	r.Use(requestContextMiddleware(observability.New(&out, "api", "info")))
	r.GET("/healthz", func(c *gin.Context) { c.Status(200) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	if out.Len() != 0 {
		t.Fatal("successful health request at info", out.String())
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/SECRET-PATH", nil))
	if out.Len() == 0 || strings.Contains(out.String(), "SECRET-PATH") {
		t.Fatal("unknown path logging", out.String())
	}
}
