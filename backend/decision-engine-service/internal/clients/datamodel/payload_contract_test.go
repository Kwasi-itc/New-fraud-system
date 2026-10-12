package datamodel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModelPreservesPayloadValidationContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data_model":{"revision_id":"rev-2","ingestion_contract":{"record_lookup_field":"object_id","managed_system_fields":["id","updated_at"]},"tables":{"wallets":{"name":"wallets","archived":true,"fields":{"status":{"name":"status","data_type":"string","nullable":true,"archived":true,"is_enum":true,"enum_values":[{"value":"verified"}]}}}}}}`))
	}))
	defer server.Close()
	model, err := NewHTTPClient(server.URL, time.Second).GetTenantModel(context.Background(), "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	field := model.Tables["wallets"].Fields["status"]
	if !model.Tables["wallets"].Archived || !field.Nullable || !field.Archived || !field.IsEnum || field.EnumValues[0] != "verified" || len(model.ManagedSystemFields) != 2 {
		t.Fatalf("contract lost: %#v", model)
	}
}
