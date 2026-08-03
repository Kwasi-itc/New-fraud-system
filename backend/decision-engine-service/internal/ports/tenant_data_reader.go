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

// AggregateCache stores immutable, generation-keyed aggregate components.
// Implementations must not apply a TTL; stale generations become unreachable.
type AggregateCache interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Set(ctx context.Context, key string, value []byte) error
	GetMany(ctx context.Context, keys []string) (map[string][]byte, error)
	SetMany(ctx context.Context, values map[string][]byte) error
}

// AggregateFallbackPolicy lets direct readers prohibit the evaluator's bounded
// in-memory fallback, which is incomplete for large tenant tables.
type AggregateFallbackPolicy interface {
	AllowLocalAggregateFallback() bool
}
