package ports

import "context"

type MutationStore interface {
	Tenants() TenantRepository
	Tables() TableRepository
	Fields() FieldRepository
	Links() LinkRepository
	Pivots() PivotRepository
	TableOptions() TableOptionsRepository
	SchemaChanges() SchemaChangeRepository
	SchemaManager() SchemaManager
}

type TransactionManager interface {
	Run(ctx context.Context, fn func(MutationStore) error) error
}
