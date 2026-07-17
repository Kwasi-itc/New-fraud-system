package ports

import (
	"context"

	"github.com/jackc/pgx/v5"
)

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
	RawTx() pgx.Tx
}

type TransactionManager interface {
	Run(ctx context.Context, fn func(MutationStore) error) error
}
