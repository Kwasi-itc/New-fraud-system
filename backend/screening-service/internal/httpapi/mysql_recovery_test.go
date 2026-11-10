package httpapi

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMySQLRecoveryAndLoggingDoNotExposeCredentials(t *testing.T) {
	secret := "private-operator-capability-must-not-appear"
	var output bytes.Buffer
	log := slog.New(slog.NewTextHandler(&output, nil))
	r := gin.New()
	r.Use(mysqlRequestLog(log, newServiceMetrics()), mysqlRecovery(log))
	r.GET("/local/evidence/:capability", func(*gin.Context) { panic(secret) })
	req := httptest.NewRequest("GET", "/local/evidence/"+secret, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Screening-Operator-Token", secret)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 500 || strings.Contains(output.String(), secret) || strings.Contains(rec.Body.String(), secret) {
		t.Fatal("credentials leaked through recovery or request logs")
	}
	req = httptest.NewRequest("GET", "/unknown/"+secret, nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if strings.Contains(output.String(), secret) {
		t.Fatal("unmatched path capability leaked to logs")
	}
}
