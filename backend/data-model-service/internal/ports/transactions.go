package ports

import "context"

type MutationStore interface {
	Tenants() TenantRepository
	Tables() TableRepository
	Fields() FieldRepository
	FieldEnumValues() FieldEnumValueRepository
	Links() LinkRepository
	Pivots() PivotRepository
	TableOptions() TableOptionsRepository
	NavigationOptions() NavigationOptionRepository
	SchemaChanges() SchemaChangeRepository
	TenantSchemaMigrations() TenantSchemaMigrationRepository
	IndexJobs() IndexJobRepository
	SchemaManager() SchemaManager
}

type TransactionManager interface {
	Run(ctx context.Context, fn func(MutationStore) error) error
}
