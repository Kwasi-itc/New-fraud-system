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
	ID            string
	Name          string
	Fields        map[string]TenantModelField
	LinksToSingle map[string]TenantModelLink
}

type TenantModelField struct {
	Name                        string
	Type                        string
	DistributionCategory        string
	ClassificationSource        string
	ClassificationPolicyVersion string
	ExpectedSameValueRows       float64
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

type FieldDistributionSuggestion struct {
	SuggestedCategory     string
	Reason                string
	RowsAnalyzed          int64
	NonNullRows           int64
	DistinctValues        int64
	ExpectedSameValueRows float64
	PolicyVersion         string
}

type DistributionSuggestionReader interface {
	AnalyzeStoredField(ctx context.Context, tenantID, tableName, fieldName string) (FieldDistributionSuggestion, error)
}
