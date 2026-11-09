package datamodel

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScopedIndexJobClientRejectsIncompleteForeignAndOverflow(t *testing.T) {
	tenant, table, job := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, tc := range []struct {
		name    string
		payload any
		valid   bool
	}{
		{"valid", map[string]any{"index_jobs": []any{map[string]any{"id": job, "tenant_id": tenant, "table_id": table, "table_name": "wallets", "status": "pending"}}}, true},
		{"empty", map[string]any{"index_jobs": []any{}}, true},
		{"missing", map[string]any{}, false},
		{"invalid id", map[string]any{"index_jobs": []any{map[string]any{"id": "invalid", "tenant_id": tenant, "table_id": table}}}, false},
		{"zero id", map[string]any{"index_jobs": []any{map[string]any{"id": uuid.Nil.String(), "tenant_id": tenant, "table_id": table}}}, false},
		{"foreign", map[string]any{"index_jobs": []any{map[string]any{"id": job, "tenant_id": uuid.NewString(), "table_id": table}}}, false},
		{"wrong table", map[string]any{"index_jobs": []any{map[string]any{"id": job, "tenant_id": tenant, "table_id": uuid.NewString()}}}, false},
		{"overflow", map[string]any{"index_jobs": []any{map[string]any{}, map[string]any{}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/tenants/"+tenant+"/index-jobs/scoped" || r.URL.Query().Get("table_id") != table || r.URL.Query().Get("limit") != "1" {
					t.Error("incorrect scoped query", r.URL)
				}
				json.NewEncoder(w).Encode(tc.payload)
			}))
			defer server.Close()
			client := NewHTTPClient(server.URL, time.Second)
			jobs, err := client.ListIndexJobsForTables(context.Background(), tenant, []string{table}, 1)
			if tc.name == "valid" && (len(jobs) != 1 || jobs[0].TenantID != tenant || jobs[0].TableID != table) {
				t.Fatal("owner identity discarded", jobs)
			}
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}
