package ports

import "context"

type TenantRecord struct {
	ObjectID   string
	ObjectType string
	Fields     map[string]any
}

type TenantDataReader interface {
	GetRecord(ctx context.Context, tenantID, objectType, objectID string) (TenantRecord, error)
	ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]TenantRecord, error)
	QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]TenantRecord, error)
}
