package ports

import "context"

type TenantRecord struct {
	ObjectID   string
	ObjectType string
	Fields     map[string]any
}

type AggregateQuery struct {
	ObjectType string           `json:"object_type"`
	Aggregate  string           `json:"aggregate"`
	Field      string           `json:"field,omitempty"`
	Filter     *AggregateFilter `json:"filter,omitempty"`
}

type AggregateFilter struct {
	Kind     string            `json:"kind,omitempty"`
	Operator string            `json:"operator,omitempty"`
	Children []AggregateFilter `json:"children,omitempty"`
	Field    string            `json:"field,omitempty"`
	Op       string            `json:"op,omitempty"`
	Value    any               `json:"value,omitempty"`
}

type TenantDataReader interface {
	GetRecord(ctx context.Context, tenantID, objectType, objectID string) (TenantRecord, error)
	ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]TenantRecord, error)
	QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]TenantRecord, error)
	AggregateRecords(ctx context.Context, tenantID string, query AggregateQuery) (any, error)
}

// AggregateBatchReader evaluates a bounded set of aggregates over the exact
// same table and predicate in one fresh authorized read. Results preserve order;
// errors return no provisional values. Implementations must reject mixed scope.
type AggregateBatchReader interface {
	AggregateRecordsBatch(context.Context, string, []AggregateQuery) ([]any, error)
}

// ModelScopedReader binds direct reads to the same tenant model used for object preparation.
// HTTP readers retain their owning service's independent model contract.
type ModelScopedReader interface {
	WithModel(tenantID string, model TenantModel) TenantDataReader
}
