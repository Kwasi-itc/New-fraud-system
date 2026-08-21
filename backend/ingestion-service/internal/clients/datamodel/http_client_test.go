package datamodel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHTTPClientGetPublishedDataModelMapsContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data_model": {
				"revision_id": "rev_123",
				"ingestion_contract": {
					"tenant_status": "active",
					"writable": true,
					"managed_system_fields": ["object_id", "updated_at", "valid_from", "valid_until"],
					"record_lookup_field": "object_id",
					"partial_updates": true
				},
				"tables": {
					"transactions": {
						"id": "11111111-1111-1111-1111-111111111111",
						"name": "transactions",
						"description": "Transaction records",
						"alias": "Transactions",
						"semantic_type": "event",
						"caption_field": "object_id",
						"storage_class": "event",
						"event_time_field": "date",
						"event_schema_revision": "event_schema_123",
						"event_schema_locked_at": "2026-08-20T12:00:00Z",
						"archived": false,
						"fields": {
							"status": {
								"id": "22222222-2222-2222-2222-222222222222",
								"name": "status",
								"description": "Transaction status",
								"data_type": "string",
								"nullable": false,
								"is_enum": true,
								"is_unique": false,
								"archived": false,
								"enum_values": [
									{
										"id": "33333333-3333-3333-3333-333333333333",
										"value": "pending",
										"label": "Pending"
									}
								]
							}
						}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	model, err := client.GetPublishedDataModel(context.Background(), uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("GetPublishedDataModel returned error: %v", err)
	}

	if model.RevisionID != "rev_123" {
		t.Fatalf("expected revision_id rev_123, got %s", model.RevisionID)
	}
	if !model.Writable || model.TenantStatus != "active" {
		t.Fatalf("unexpected ingestion contract: %+v", model)
	}
	if model.RecordLookupField != "object_id" {
		t.Fatalf("expected record lookup field object_id, got %s", model.RecordLookupField)
	}
	statusField := model.Tables["transactions"].Fields["status"]
	if len(statusField.EnumValues) != 1 || statusField.EnumValues[0].Value != "pending" {
		t.Fatalf("expected enum value mapping, got %+v", statusField.EnumValues)
	}
	transactions := model.Tables["transactions"]
	if transactions.EventSchemaRevision != "event_schema_123" || transactions.EventSchemaLockedAt == nil {
		t.Fatalf("expected event schema lock metadata, got %+v", transactions)
	}
}

func TestHTTPClientLocksEventTableSchema(t *testing.T) {
	t.Parallel()
	tenantID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	tableID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tenants/"+tenantID.String()+"/tables/"+tableID.String()+"/event-schema-lock" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode lock request: %v", err)
		}
		if body["schema_revision"] != "schema-123" {
			t.Fatalf("schema_revision = %q", body["schema_revision"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := NewHTTPClient(server.URL, time.Second).LockEventTableSchema(context.Background(), tenantID, tableID, "schema-123"); err != nil {
		t.Fatalf("LockEventTableSchema() error = %v", err)
	}
}

func TestHTTPClientGetPublishedDataModelRequiresRevisionID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data_model": {
				"revision_id": "",
				"ingestion_contract": {
					"tenant_status": "active",
					"writable": true,
					"managed_system_fields": [],
					"record_lookup_field": "object_id",
					"partial_updates": true
				},
				"tables": {}
			}
		}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	_, err := client.GetPublishedDataModel(context.Background(), uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	if err == nil {
		t.Fatal("expected missing revision_id to return an error")
	}
}

func TestHTTPClientGetPublishedDataModelReturnsDependencyErrorOnNon200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	_, err := client.GetPublishedDataModel(context.Background(), uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	if err == nil {
		t.Fatal("expected non-200 response to return an error")
	}
	if !strings.Contains(err.Error(), "unexpected status from data-model-service") {
		t.Fatalf("error = %v, want unexpected status from data-model-service", err)
	}
}
