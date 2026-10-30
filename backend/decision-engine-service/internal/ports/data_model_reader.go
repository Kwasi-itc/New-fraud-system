package ports

import "context"

type TenantModel struct {
	RevisionID          string
	RecordLookupField   string
	ManagedSystemFields []string
	Tables              map[string]TenantModelTable
}

type ManagedIndexJob struct {
	ID        string
	TableName string
	IndexType string
	Status    string
	Columns   []string
}

type TenantModelTable struct {
	ID string
	// PublishedVersion is the table-scoped owner publication token, not the
	// global model revision. Required by the MySQL physical-registry reader;
	// existing PostgreSQL/HTTP readers do not manufacture a value for it.
	PublishedVersion string
	Name             string
	Archived         bool
	Fields           map[string]TenantModelField
	LinksToSingle    map[string]TenantModelLink
}

type TenantModelField struct {
	Name       string
	Type       string
	Nullable   bool
	Archived   bool
	IsEnum     bool
	EnumValues []string
}

type TenantModelLink struct {
	Name            string
	ParentTableName string
	ParentFieldName string
	ChildTableName  string
	ChildFieldName  string
}

// TenantModelReader is the read-only owner publication boundary. Evaluation
// must not require fake index-management methods from a read-only provider.
type TenantModelReader interface {
	GetTenantModel(ctx context.Context, tenantID string) (TenantModel, error)
}

type DataModelReader interface {
	TenantModelReader
	CreateIndexJob(ctx context.Context, tenantID, tableID, indexType string, columns []string, requestedByOperation string) (ManagedIndexJob, error)
	ListIndexJobs(ctx context.Context, tenantID string) ([]ManagedIndexJob, error)
	RetryIndexJob(ctx context.Context, jobID string) error
}
