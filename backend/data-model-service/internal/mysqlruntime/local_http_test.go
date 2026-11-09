package mysqlruntime

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"gopkg.in/yaml.v3"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestLocalHTTPBoundary(t *testing.T) {
	if _, err := (mysqlstore.DateTime{Time: (localClock{}).Now()}).Value(); err != nil {
		t.Fatal(err)
	}
	cfg := mysql.NewConfig()
	cfg.User = "unused"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	if _, err := NewLocalHTTP(db, "", "http://localhost:8080"); err == nil {
		t.Fatal("implicit no-auth accepted")
	}
	if _, err := NewLocalHTTP(db, "1", "https://public.example"); err == nil {
		t.Fatal("public origin accepted")
	}
	if err := localAuthority(context.Background(), nil); err == nil {
		t.Fatal("missing server authority accepted")
	}
	handler, err := NewLocalHTTP(db, "1", "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, origin, site string
		status             int
	}{
		{"/docs", "", "", 200},
		{"/openapi.json", "", "", 200},
		{"/openapi.yaml", "", "", 200},
		{"/healthz", "", "", 200},
		{"/v1/tenants/not-a-uuid", "", "", 400},
		{"/v1/tenants/00000000-0000-0000-0000-000000000000", "", "", 400},
		{"/v1/tenants/984df53a-6868-4ef1-877d-dc4f5577a678", "http://evil.example", "", 403},
		{"/v1/tenants/not-a-uuid/data-model", "", "", 400},
		{"/v1/tenants/00000000-0000-0000-0000-000000000000/data-model", "", "", 400},
		{"/docs", "http://evil.example", "", 403},
		{"/docs", "", "cross-site", 403},
	} {
		t.Run(tc.path+tc.origin+tc.site, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.site)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status %d: %s", res.Code, res.Body.String())
			}
			if tc.path == "/openapi.json" {
				var spec map[string]any
				if err := json.Unmarshal(res.Body.Bytes(), &spec); err != nil {
					t.Fatal(err)
				}
				if spec["openapi"] != "3.0.3" {
					t.Fatal("invalid spec")
				}
				var source map[string]any
				if err := yaml.Unmarshal(httpapi.OpenAPISpec(), &source); err != nil {
					t.Fatal(err)
				}
				// Normalize JSON number representations before comparing the entire
				// contract, including all operations, parameters and components.
				encoded, err := json.Marshal(source)
				if err != nil {
					t.Fatal(err)
				}
				var expected map[string]any
				if err := json.Unmarshal(encoded, &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(spec, expected) {
					t.Fatal("local documentation differs from complete API contract")
				}
			}
			if tc.path == "/openapi.yaml" && !bytes.Equal(res.Body.Bytes(), httpapi.OpenAPISpec()) {
				t.Fatal("YAML contract changed")
			}
		})
	}
}
