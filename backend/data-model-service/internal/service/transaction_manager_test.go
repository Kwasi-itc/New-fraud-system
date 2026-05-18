package service

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type stubTransactionManager struct {
	store ports.MutationStore
}

func (m stubTransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	return fn(m.store)
}

type stubMutationStore struct {
	tenants                ports.TenantRepository
	tables                 ports.TableRepository
	fields                 ports.FieldRepository
	fieldEnumValues        ports.FieldEnumValueRepository
	links                  ports.LinkRepository
	pivots                 ports.PivotRepository
	tableOptions           ports.TableOptionsRepository
	navigationOptions      ports.NavigationOptionRepository
	schemaChanges          ports.SchemaChangeRepository
	tenantSchemaMigrations ports.TenantSchemaMigrationRepository
	indexJobs              ports.IndexJobRepository
	schemaManager          ports.SchemaManager
}

func (s stubMutationStore) Tenants() ports.TenantRepository             { return s.tenants }
func (s stubMutationStore) Tables() ports.TableRepository               { return s.tables }
func (s stubMutationStore) Fields() ports.FieldRepository               { return s.fields }
func (s stubMutationStore) FieldEnumValues() ports.FieldEnumValueRepository {
	return s.fieldEnumValues
}
func (s stubMutationStore) Links() ports.LinkRepository                 { return s.links }
func (s stubMutationStore) Pivots() ports.PivotRepository               { return s.pivots }
func (s stubMutationStore) TableOptions() ports.TableOptionsRepository  { return s.tableOptions }
func (s stubMutationStore) NavigationOptions() ports.NavigationOptionRepository { return s.navigationOptions }
func (s stubMutationStore) SchemaChanges() ports.SchemaChangeRepository { return s.schemaChanges }
func (s stubMutationStore) TenantSchemaMigrations() ports.TenantSchemaMigrationRepository {
	return s.tenantSchemaMigrations
}
func (s stubMutationStore) IndexJobs() ports.IndexJobRepository { return s.indexJobs }
func (s stubMutationStore) SchemaManager() ports.SchemaManager { return s.schemaManager }
