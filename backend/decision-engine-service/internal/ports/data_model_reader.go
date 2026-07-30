package ports

import (
	"context"
	"time"
)

type TenantModel struct {
	RevisionID         string
	PhysicalSchemaName string
	RecordLookupField  string
	Tables             map[string]TenantModelTable
	LogicalBuckets     []LogicalBucketDefinition
}

type ManagedIndexJob struct {
	ID           string
	TableName    string
	IndexType    string
	Status       string
	Columns      []string
	Purpose      string
	SpecHash     string
	OwnerService string
}

type LogicalBucketDefinition struct {
	ID                 string
	TableID            string
	TimestampFieldName string
	Grain              string
	Timezone           string
	SealDelay          time.Duration
	DefinitionVersion  int
	Status             string
	CacheEligibleAt    *time.Time
	MaintenanceUntil   *time.Time
}

type TenantModelTable struct {
	ID            string
	Name          string
	Fields        map[string]TenantModelField
	LinksToSingle map[string]TenantModelLink
}

type TenantModelField struct {
	Name string
	Type string
}

type TenantModelLink struct {
	Name            string
	ParentTableName string
	ParentFieldName string
	ChildTableName  string
	ChildFieldName  string
}

type DataModelReader interface {
	GetTenantModel(ctx context.Context, tenantID string) (TenantModel, error)
	CreateIndexJob(ctx context.Context, tenantID, tableID, indexType string, columns []string, requestedByOperation string) (ManagedIndexJob, error)
	ListIndexJobs(ctx context.Context, tenantID string) ([]ManagedIndexJob, error)
	RetryIndexJob(ctx context.Context, jobID string) error
}
