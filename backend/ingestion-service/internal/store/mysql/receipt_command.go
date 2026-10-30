package mysql

import "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"

// NewReceiptRequest derives the staged v1 identity without importing a driver
// into the domain. Key validation and current authorization run in Execute.
func NewReceiptRequest(command ingestion.ReceiptCommand, key string, maxCommandBytes int) (ReceiptRequest, error) {
	_, hash, err := ingestion.CanonicalReceiptCommand(command, maxCommandBytes)
	if err != nil {
		return ReceiptRequest{}, err
	}
	return ReceiptRequest{TenantID: command.TenantID, TableID: command.TableID, Key: key, Mode: string(command.Mode), Kind: command.Kind, Hash: hash}, nil
}
