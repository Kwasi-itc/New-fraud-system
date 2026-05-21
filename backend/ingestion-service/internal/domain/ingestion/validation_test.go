package ingestion

import (
	"testing"

	"github.com/google/uuid"
)

func TestValidateRecordCreateRequiresNonNullableFields(t *testing.T) {
	t.Parallel()

	model := PublishedDataModel{
		TenantID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RevisionID:          "rev",
		Writable:            true,
		RecordLookupField:   "object_id",
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables: map[string]ObjectSchema{
			"transactions": {
				Name: "transactions",
				Fields: map[string]FieldSchema{
					"amount": {Name: "amount", DataType: "float", Nullable: false},
					"note":   {Name: "note", DataType: "string", Nullable: true},
				},
			},
		},
	}

	_, _, errors := ValidateRecord(model, "transactions", map[string]any{
		"object_id": "txn-1",
	}, ModeCreate)
	if len(errors) != 1 || errors[0].Field != "amount" {
		t.Fatalf("expected missing required amount error, got %+v", errors)
	}
}

func TestValidateRecordPatchAllowsOmittedNonNullableFields(t *testing.T) {
	t.Parallel()

	model := PublishedDataModel{
		TenantID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RevisionID:          "rev",
		Writable:            true,
		RecordLookupField:   "object_id",
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables: map[string]ObjectSchema{
			"transactions": {
				Name: "transactions",
				Fields: map[string]FieldSchema{
					"amount": {Name: "amount", DataType: "float", Nullable: false},
					"note":   {Name: "note", DataType: "string", Nullable: true},
				},
			},
		},
	}

	normalized, objectID, errors := ValidateRecord(model, "transactions", map[string]any{
		"object_id": "txn-1",
		"note":      "patched",
	}, ModePatch)
	if len(errors) != 0 {
		t.Fatalf("expected patch to allow omitted required fields, got %+v", errors)
	}
	if objectID != "txn-1" {
		t.Fatalf("expected object id txn-1, got %s", objectID)
	}
	if normalized["note"] != "patched" {
		t.Fatalf("expected normalized patch field, got %+v", normalized)
	}
}

func TestValidateRecordRejectsUnknownAndManagedFields(t *testing.T) {
	t.Parallel()

	model := PublishedDataModel{
		TenantID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RevisionID:          "rev",
		Writable:            true,
		RecordLookupField:   "object_id",
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables: map[string]ObjectSchema{
			"transactions": {
				Name: "transactions",
				Fields: map[string]FieldSchema{
					"amount": {Name: "amount", DataType: "float", Nullable: false},
				},
			},
		},
	}

	_, _, errors := ValidateRecord(model, "transactions", map[string]any{
		"object_id":  "txn-1",
		"updated_at": "2026-05-20T10:00:00Z",
		"unknown":    "value",
		"amount":     10.0,
	}, ModePatch)
	if len(errors) != 2 {
		t.Fatalf("expected two validation errors, got %+v", errors)
	}
}
