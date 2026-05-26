package ports

import "context"

type TenantModel struct {
	RevisionID        string
	RecordLookupField string
	Tables            map[string]TenantModelTable
}

type ManagedIndexJob struct {
	ID        string
	TableName string
	IndexType string
	Status    string
	Columns   []string
}

type TenantModelTable struct {
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
	ListIndexJobs(ctx context.Context, tenantID string) ([]ManagedIndexJob, error)
	RetryIndexJob(ctx context.Context, jobID string) error
}
