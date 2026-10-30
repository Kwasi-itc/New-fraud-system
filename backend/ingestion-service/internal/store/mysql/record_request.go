package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
)

var ErrCommandInput = errors.New("invalid bounded ingestion command")

// ExecuteRequest uses a caller-supplied private UUID once for an unkeyed request.
// It is never regenerated during reconciliation, nor mixed with public keys.
func (o *KeyedRecordOwner) ExecuteRequest(ctx context.Context, mode ingestion.Mode, kind string, records []map[string]any, key *string, privateID uuid.UUID) (ReceiptResult, error) {
	if o == nil {
		return ReceiptResult{}, ErrCommandInput
	}
	command := ingestion.ReceiptCommand{TenantID: o.receipts.tenant, TableID: o.table, Mode: mode, Kind: kind, Records: records}
	if _, _, err := ingestion.CanonicalReceiptCommand(command, o.limits.CommandBytes); err != nil {
		return ReceiptResult{}, errors.Join(ErrCommandInput, err)
	}
	if key != nil {
		return o.Execute(ctx, command, *key)
	}
	if privateID == uuid.Nil {
		return ReceiptResult{}, ErrCommandInput
	}
	copyOwner := *o
	manager := *o.receipts
	manager.bucketTable = manager.schema + ".ingestion_execution_buckets"
	manager.receiptTable = manager.schema + ".ingestion_execution_receipts"
	copyOwner.receipts = &manager
	return copyOwner.Execute(ctx, command, privateID.String())
}
