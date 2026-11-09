package httpapi_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi"
	R "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/mysqlruntime"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
)

func TestMySQLRouterServesDocsWithoutDatabaseOrCredential(t *testing.T) {
	s := &R.Service{}
	r, err := httpapi.NewWriteWorkRouter(slog.Default(), httpapi.RouterConfig{AuthMode: "token", AuthToken: "test-token", WritePathConcurrencyLimit: 1}, s, s, R.Uploads{Service: s}, R.Deferred{Service: s}, 1<<20, store.WithServiceCredential, func(context.Context) error { t.Fatal("docs queried readiness"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{"/docs": "SwaggerUIBundle", "/openapi.yaml": "openapi: 3.0.3", "/redoc": "redoc.standalone.js"} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 || !strings.Contains(response.Body.String(), content) {
			t.Fatalf("%s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"/v1/upload-logs/00000000-0000-0000-0000-000000000001", "/v1/admin/deferred-ingest-metrics"} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 401 {
			t.Fatalf("documentation registration bypassed API authentication for %s: %d", path, response.Code)
		}
	}
}
