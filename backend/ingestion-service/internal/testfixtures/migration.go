// Package testfixtures contains portable source fixtures shared by migration tests.
// It has no database setup, environment access or production callers.
package testfixtures

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
)

// SourceRecord retains the seeded record and expected values from the existing
// PostgreSQL migration characterization, including its upload representation.
type SourceRecord struct {
	ObjectID, Status string
	Amount           float64
}

func MigrationRecord() SourceRecord { return SourceRecord{"record-1", "pending", 10.5} }
func (r SourceRecord) Payload() map[string]any {
	return map[string]any{"object_id": r.ObjectID, "amount": r.Amount, "status": r.Status}
}
func (r SourceRecord) CSV() []byte {
	return []byte(fmt.Sprintf("object_id,amount,status\n%s,%g,%s\n", r.ObjectID, r.Amount, r.Status))
}
func SourceTransactionModel(tenantID uuid.UUID) ingestion.PublishedDataModel {
	return ingestion.PublishedDataModel{
		TenantID:            tenantID,
		RevisionID:          "rev_transactions_v1",
		TenantStatus:        "active",
		Writable:            true,
		RecordLookupField:   "object_id",
		PartialUpdates:      true,
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {
				ID:           uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				Name:         "transactions",
				Description:  "Transaction records",
				CaptionField: "object_id",
				Fields: map[string]ingestion.FieldSchema{
					"amount": {
						ID:       uuid.MustParse("44444444-4444-4444-4444-444444444444"),
						Name:     "amount",
						DataType: "float",
						Nullable: false,
					},
					"status": {
						ID:       uuid.MustParse("55555555-5555-5555-5555-555555555555"),
						Name:     "status",
						DataType: "string",
						Nullable: false,
						IsEnum:   true,
						EnumValues: []ingestion.EnumValue{
							{ID: uuid.MustParse("66666666-6666-6666-6666-666666666666"), Value: "pending", Label: "Pending"},
							{ID: uuid.MustParse("77777777-7777-7777-7777-777777777777"), Value: "approved", Label: "Approved"},
						},
					},
				},
			},
		},
	}
}
