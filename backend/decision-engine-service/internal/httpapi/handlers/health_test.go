package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type readinessPingerStub struct {
	err error
}

func (s readinessPingerStub) Ping(context.Context) error { return s.err }

func TestReadyzRejectsUnavailableAggregateFactStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHealthHandler(slog.Default(), nil, ReadinessDependency{
		Name:   "aggregate_fact_store",
		Pinger: readinessPingerStub{err: errors.New("unavailable")},
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)

	handler.Readyz(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), `"dependency":"aggregate_fact_store"`) {
		t.Fatalf("body = %s, want aggregate fact dependency", recorder.Body.String())
	}
}
