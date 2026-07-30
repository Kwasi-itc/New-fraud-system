package datamodel

import (
	"context"
	"net/http"
	"net/http/httptest"
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
				"physical_schema_name": "tenant_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"ingestion_contract": {
					"tenant_status": "active",
					"writable": true,
					"managed_system_fields": ["object_id", "updated_at", "valid_from", "valid_until"],
					"record_lookup_field": "object_id",
					"partial_updates": true
				},
				"logical_bucket_definitions": [{
					"id": "44444444-4444-4444-4444-444444444444",
					"table_id": "11111111-1111-1111-1111-111111111111",
					"timestamp_field_id": "55555555-5555-5555-5555-555555555555",
					"timestamp_field_name": "occurred_at",
					"grain": "daily",
					"timezone": "Africa/Accra",
					"seal_delay_seconds": 172800,
					"definition_version": 1,
					"status": "active",
					"cache_eligible_at": "2026-07-01T00:00:00Z"
				}],
				"tables": {
					"transactions": {
						"id": "11111111-1111-1111-1111-111111111111",
						"name": "transactions",
						"description": "Transaction records",
						"alias": "Transactions",
						"semantic_type": "event",
						"caption_field": "object_id",
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
	if model.PhysicalSchemaName != "tenant_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("physical schema = %q", model.PhysicalSchemaName)
	}
	if len(model.LogicalBuckets) != 1 ||
		model.LogicalBuckets[0].TimestampFieldName != "occurred_at" ||
		model.LogicalBuckets[0].SealDelay != 48*time.Hour {
		t.Fatalf("logical bucket mapping = %+v", model.LogicalBuckets)
	}
	statusField := model.Tables["transactions"].Fields["status"]
	if len(statusField.EnumValues) != 1 || statusField.EnumValues[0].Value != "pending" {
		t.Fatalf("expected enum value mapping, got %+v", statusField.EnumValues)
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
