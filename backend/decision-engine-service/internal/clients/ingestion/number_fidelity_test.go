package ingestion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestRecordReadsPreserveLargeIntegerText(t *testing.T) {
	record := `{"object_id":"w","object_type":"wallets","fields":{"count":9223372036854775807}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/tenants/t/records/wallets/w" {
			_, _ = w.Write([]byte(`{"record":` + record + `}`))
		} else {
			_, _ = w.Write([]byte(`{"records":[` + record + `]}`))
		}
	}))
	defer server.Close()
	client := NewHTTPClient(server.URL, time.Second)
	ctx := context.Background()
	get, err := client.GetRecord(ctx, "t", "wallets", "w")
	if err != nil {
		t.Fatal(err)
	}
	list, err := client.ListRecords(ctx, "t", "wallets", 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := client.QueryRecords(ctx, "t", "wallets", "object_id", "w", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range append([]ports.TenantRecord{get}, append(list, query...)...) {
		if value, ok := row.Fields["count"].(json.Number); !ok || string(value) != "9223372036854775807" {
			t.Fatalf("numeric precision lost: %#v", row.Fields)
		}
	}
}
