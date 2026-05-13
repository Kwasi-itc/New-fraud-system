package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
)

func TestIntegrationCoreLifecyclePersistsMetadataAndTenantDDL(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	ctx := context.Background()
	pool := integrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantRepo := storepostgres.NewTenantRepository(pool)
	tableRepo := storepostgres.NewTableRepository(pool)
	fieldRepo := storepostgres.NewFieldRepository(pool)
	linkRepo := storepostgres.NewLinkRepository(pool)
	pivotRepo := storepostgres.NewPivotRepository(pool)
	optionsRepo := storepostgres.NewTableOptionsRepository(pool)
	readRepo := storepostgres.NewDataModelReadRepository(pool)
	schemaChanges := storepostgres.NewSchemaChangeRepository(pool)
	tenantSchemaMigrations := storepostgres.NewTenantSchemaMigrationRepository(pool)
	schemaManager := tenantdbpostgres.NewSchemaManager(pool)
	txManager := storepostgres.NewTransactionManager(pool)

	idGen := &sequenceIDGenerator{values: integrationUUIDSequence(40)}
	clock := fixedIntegrationClock{now: time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)}

	tenantService := NewTenantService(tenantRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	tableService := NewTableService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	linkService := NewLinkService(tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, txManager, idGen, clock)
	pivotService := NewPivotService(tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, txManager, idGen, clock)
	optionsService := NewOptionsService(tableRepo, fieldRepo, optionsRepo, schemaChanges, txManager, idGen, clock)
	readService := NewDataModelReadService(readRepo)

	record, err := tenantService.Create(ctx, tenant.CreateInput{Name: "Lifecycle Tenant"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	record, err = tenantService.Provision(ctx, record.ID)
	if err != nil {
		t.Fatalf("provision tenant: %v", err)
	}

	accounts, err := tableService.Create(ctx, CreateTableInput{
		TenantID:    record.ID,
		Name:        "accounts",
		Description: "Customer accounts",
	})
	if err != nil {
		t.Fatalf("create accounts table: %v", err)
	}
	transactions, err := tableService.Create(ctx, CreateTableInput{
		TenantID:    record.ID,
		Name:        "transactions",
		Description: "Transaction records",
	})
	if err != nil {
		t.Fatalf("create transactions table: %v", err)
	}

	accountName, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:     accounts.ID,
		Name:        "display_name",
		Description: "Friendly account name",
		DataType:    datamodel.DataTypeString,
		Nullable:    false,
	})
	if err != nil {
		t.Fatalf("create accounts.display_name: %v", err)
	}
	accountLookup, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:     transactions.ID,
		Name:        "account_id",
		Description: "Linked account external identifier",
		DataType:    datamodel.DataTypeString,
		Nullable:    false,
	})
	if err != nil {
		t.Fatalf("create transactions.account_id: %v", err)
	}
	amount, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:     transactions.ID,
		Name:        "amount",
		Description: "Transaction amount",
		DataType:    datamodel.DataTypeFloat,
		Nullable:    false,
	})
	if err != nil {
		t.Fatalf("create transactions.amount: %v", err)
	}

	accountFields, err := fieldRepo.ListByTable(ctx, accounts.ID)
	if err != nil {
		t.Fatalf("list account fields: %v", err)
	}
	transactionsFields, err := fieldRepo.ListByTable(ctx, transactions.ID)
	if err != nil {
		t.Fatalf("list transaction fields: %v", err)
	}

	accountsObjectID := findFieldByName(t, accountFields, "object_id")
	transactionsUpdatedAt := findFieldByName(t, transactionsFields, "updated_at")
	if transactionsUpdatedAt.DataType != datamodel.DataTypeTimestamp {
		t.Fatalf("expected updated_at to be timestamp, got %s", transactionsUpdatedAt.DataType)
	}

	link, err := linkService.Create(ctx, CreateLinkInput{
		TenantID:    record.ID,
		Name:        "account",
		ParentTable: accounts.ID,
		ParentField: accountsObjectID.ID,
		ChildTable:  transactions.ID,
		ChildField:  accountLookup.ID,
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	pivot, err := pivotService.Create(ctx, CreatePivotInput{
		TenantID:    record.ID,
		BaseTableID: transactions.ID,
		PathLinkIDs: []uuid.UUID{link.ID},
	})
	if err != nil {
		t.Fatalf("create pivot: %v", err)
	}

	options, err := optionsService.Upsert(ctx, datamodel.TableOptions{
		TableID:         transactions.ID,
		DisplayedFields: []uuid.UUID{accountLookup.ID, amount.ID},
		FieldOrder:      []uuid.UUID{amount.ID, accountLookup.ID},
	})
	if err != nil {
		t.Fatalf("upsert table options: %v", err)
	}

	model, err := readService.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("read assembled data model: %v", err)
	}

	assertTenantSchemaExists(t, ctx, pool, record.SchemaName)
	assertTenantTableExists(t, ctx, pool, record.SchemaName, accounts.Name)
	assertTenantTableExists(t, ctx, pool, record.SchemaName, transactions.Name)
	assertTenantColumnExists(t, ctx, pool, record.SchemaName, transactions.Name, "account_id")
	assertTenantColumnExists(t, ctx, pool, record.SchemaName, transactions.Name, "amount")
	assertUniqueIndexExistsOnColumns(t, ctx, pool, record.SchemaName, accounts.Name, "object_id")
	assertUniqueIndexExistsOnColumns(t, ctx, pool, record.SchemaName, transactions.Name, "object_id")

	assembledTransactions, ok := model.Tables["transactions"]
	if !ok {
		t.Fatal("expected transactions table in assembled data model")
	}
	if assembledTransactions.Description != "Transaction records" {
		t.Fatalf("unexpected transactions description: %s", assembledTransactions.Description)
	}
	if _, ok := assembledTransactions.Fields["account_id"]; !ok {
		t.Fatal("expected account_id field in assembled data model")
	}
	assembledLink, ok := assembledTransactions.LinksToSingle["account"]
	if !ok {
		t.Fatal("expected account link in assembled data model")
	}
	if assembledLink.ParentTableName != "accounts" || assembledLink.ChildFieldName != "account_id" {
		t.Fatalf("unexpected assembled link: %+v", assembledLink)
	}
	if assembledTransactions.Options == nil {
		t.Fatal("expected table options in assembled data model")
	}
	if !slices.Equal(assembledTransactions.Options.DisplayedFields, []uuid.UUID{accountLookup.ID, amount.ID}) {
		t.Fatalf("unexpected displayed fields: %v", assembledTransactions.Options.DisplayedFields)
	}
	if !slices.Equal(assembledTransactions.Options.FieldOrder, []uuid.UUID{amount.ID, accountLookup.ID}) {
		t.Fatalf("unexpected field order: %v", assembledTransactions.Options.FieldOrder)
	}

	if len(model.Pivots) != 1 {
		t.Fatalf("expected 1 pivot, got %d", len(model.Pivots))
	}
	if model.Pivots[0].ID != pivot.ID || model.Pivots[0].BaseTable != "transactions" {
		t.Fatalf("unexpected pivot in assembled model: %+v", model.Pivots[0])
	}
	if !slices.Equal(model.Pivots[0].PathLinks, []string{"account"}) {
		t.Fatalf("unexpected pivot path links: %v", model.Pivots[0].PathLinks)
	}

	storedOptions, err := optionsRepo.GetByTableID(ctx, transactions.ID)
	if err != nil {
		t.Fatalf("reload table options: %v", err)
	}
	if storedOptions == nil || storedOptions.ID != options.ID {
		t.Fatalf("expected stored table options, got %#v", storedOptions)
	}

	changes, err := schemaChanges.ListByTenant(ctx, record.ID)
	if err != nil {
		t.Fatalf("list schema changes: %v", err)
	}
	assertSchemaChangeOperations(t, changes,
		"create_tenant",
		"provision_tenant_schema",
		"create_table",
		"create_field",
		"create_link",
		"create_pivot",
		"upsert_table_options",
	)

	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_tenant:tenant")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "provision_tenant_schema:tenant")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_table:table")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_field:field")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_link:link")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_pivot:pivot")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "upsert_table_options:table_options")

	if accountName.Name != "display_name" {
		t.Fatalf("unexpected accounts field name: %s", accountName.Name)
	}
}

func TestIntegrationFieldUniqueUpdateRollsBackMetadataOnIndexFailure(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	ctx := context.Background()
	pool := integrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantRepo := storepostgres.NewTenantRepository(pool)
	tableRepo := storepostgres.NewTableRepository(pool)
	fieldRepo := storepostgres.NewFieldRepository(pool)
	linkRepo := storepostgres.NewLinkRepository(pool)
	pivotRepo := storepostgres.NewPivotRepository(pool)
	schemaChanges := storepostgres.NewSchemaChangeRepository(pool)
	tenantSchemaMigrations := storepostgres.NewTenantSchemaMigrationRepository(pool)
	schemaManager := tenantdbpostgres.NewSchemaManager(pool)
	txManager := storepostgres.NewTransactionManager(pool)

	idGen := sequenceIDGenerator{values: []uuid.UUID{
		uuid.MustParse("10000000-0000-0000-0000-000000000001"),
		uuid.MustParse("10000000-0000-0000-0000-000000000002"),
		uuid.MustParse("10000000-0000-0000-0000-000000000003"),
		uuid.MustParse("10000000-0000-0000-0000-000000000004"),
		uuid.MustParse("10000000-0000-0000-0000-000000000005"),
		uuid.MustParse("10000000-0000-0000-0000-000000000006"),
		uuid.MustParse("10000000-0000-0000-0000-000000000007"),
		uuid.MustParse("10000000-0000-0000-0000-000000000008"),
		uuid.MustParse("10000000-0000-0000-0000-000000000009"),
		uuid.MustParse("10000000-0000-0000-0000-000000000010"),
	}}
	clock := fixedIntegrationClock{now: time.Date(2026, 5, 12, 15, 0, 0, 0, time.UTC)}

	tenantService := NewTenantService(tenantRepo, schemaChanges, schemaManager, txManager, &idGen, clock)
	tableService := NewTableService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, &idGen, clock)
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, &idGen, clock)

	record, err := tenantService.Create(ctx, tenant.CreateInput{Name: "Integration Tenant"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	record, err = tenantService.Provision(ctx, record.ID)
	if err != nil {
		t.Fatalf("provision tenant: %v", err)
	}

	table, err := tableService.Create(ctx, CreateTableInput{
		TenantID: record.ID,
		Name:     "cases",
	})
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_tenant:tenant")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "provision_tenant_schema:tenant")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_table:table")

	field, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:  table.ID,
		Name:     "email",
		DataType: datamodel.DataTypeString,
		Nullable: false,
	})
	if err != nil {
		t.Fatalf("create field: %v", err)
	}
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "create_field:field")

	insertDuplicateTenantRows(t, ctx, pool, record.SchemaName, table.Name)

	makeUnique := true
	updated, err := fieldService.Update(ctx, UpdateFieldInput{
		FieldID:     field.ID,
		IsUnique:    &makeUnique,
		Nullable:    nil,
		IsEnum:      nil,
		Description: nil,
	})
	if err == nil {
		t.Fatal("expected unique index creation failure")
	}
	if updated.IsUnique {
		t.Fatal("expected failed update not to return persisted unique state")
	}

	storedField, err := fieldRepo.GetByID(ctx, field.ID)
	if err != nil {
		t.Fatalf("reload field: %v", err)
	}
	if storedField.IsUnique {
		t.Fatal("expected metadata rollback to preserve is_unique=false")
	}

	exists, err := uniqueIndexOnColumnExists(ctx, pool, record.SchemaName, table.Name, field.Name)
	if err != nil {
		t.Fatalf("check unique index: %v", err)
	}
	if exists {
		t.Fatal("expected unique index not to exist after failed transaction")
	}
}

func integrationDatabaseURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("DATA_MODEL_TEST_DATABASE_URL"); url != "" {
		return url
	}
	t.Skip("set DATA_MODEL_TEST_DATABASE_URL to run PostgreSQL integration tests")
	return ""
}

func integrationPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := storepostgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect integration pool: %v", err)
	}
	return pool
}

func resetIntegrationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseURL string) {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT nspname
		FROM pg_namespace
		WHERE nspname = 'core' OR nspname LIKE 'tenant_%'
	`)
	if err != nil {
		t.Fatalf("list schemas: %v", err)
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			t.Fatalf("scan schema: %v", err)
		}
		schemas = append(schemas, schema)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schemas: %v", err)
	}

	for _, schema := range schemas {
		if _, err := pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgx.Identifier{schema}.Sanitize())); err != nil {
			t.Fatalf("drop schema %s: %v", schema, err)
		}
	}

	runMetadataMigrations(t, databaseURL)
}

func runMetadataMigrations(t *testing.T, databaseURL string) {
	t.Helper()

	_, fileName, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}
	rootDir := filepath.Clean(filepath.Join(filepath.Dir(fileName), "..", ".."))
	migrationsPath := "file://" + filepath.ToSlash(filepath.Join(rootDir, "internal", "migrations", "metadata"))

	m, err := migrate.New(migrationsPath, databaseURL)
	if err != nil {
		t.Fatalf("create migrate client: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}
}

func insertDuplicateTenantRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) {
	t.Helper()
	now := time.Date(2026, 5, 12, 15, 30, 0, 0, time.UTC)
	query := fmt.Sprintf(
		"INSERT INTO %s (id, object_id, updated_at, email) VALUES ($1, $2, $3, $4), ($5, $6, $7, $8)",
		pgx.Identifier{schemaName, tableName}.Sanitize(),
	)
	_, err := pool.Exec(
		ctx,
		query,
		uuid.MustParse("20000000-0000-0000-0000-000000000001"), "obj-1", now, "dup@example.com",
		uuid.MustParse("20000000-0000-0000-0000-000000000002"), "obj-2", now, "dup@example.com",
	)
	if err != nil {
		t.Fatalf("insert duplicate tenant rows: %v", err)
	}
}

func uniqueIndexOnColumnExists(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName, columnName string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_indexes
			WHERE schemaname = $1
			  AND tablename = $2
			  AND indexdef LIKE 'CREATE UNIQUE INDEX%'
			  AND indexdef LIKE '%' || $3 || '%'
		)
	`, schemaName, tableName, columnName).Scan(&exists)
	return exists, err
}

func integrationUUIDSequence(count int) []uuid.UUID {
	values := make([]uuid.UUID, count)
	for i := 0; i < count; i++ {
		values[i] = uuid.MustParse(fmt.Sprintf("10000000-0000-0000-0000-%012d", i+1))
	}
	return values
}

func assertTenantSchemaMigrationVersionExists(
	t *testing.T,
	ctx context.Context,
	repository storepostgres.TenantSchemaMigrationRepository,
	tenantID uuid.UUID,
	version string,
) {
	t.Helper()
	migrations, err := repository.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list tenant schema migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == version {
			return
		}
	}
	t.Fatalf("expected tenant schema migration version %s to exist, got %v", version, migrations)
}

func assertSchemaChangeOperations(t *testing.T, changes []datamodel.SchemaChange, expected ...string) {
	t.Helper()
	operations := make([]string, 0, len(changes))
	for _, change := range changes {
		operations = append(operations, change.Operation)
	}
	for _, operation := range expected {
		if !slices.Contains(operations, operation) {
			t.Fatalf("expected schema change operation %s, got %v", operation, operations)
		}
	}
}

func findFieldByName(t *testing.T, fields []datamodel.Field, name string) datamodel.Field {
	t.Helper()
	for _, field := range fields {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("expected field %s in %v", name, fields)
	return datamodel.Field{}
}

func assertTenantSchemaExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName string) {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_namespace
			WHERE nspname = $1
		)
	`, schemaName).Scan(&exists)
	if err != nil {
		t.Fatalf("check tenant schema existence: %v", err)
	}
	if !exists {
		t.Fatalf("expected tenant schema %s to exist", schemaName)
	}
}

func assertTenantTableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = $1
			  AND table_name = $2
		)
	`, schemaName, tableName).Scan(&exists)
	if err != nil {
		t.Fatalf("check tenant table existence: %v", err)
	}
	if !exists {
		t.Fatalf("expected tenant table %s.%s to exist", schemaName, tableName)
	}
}

func assertTenantColumnExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName, columnName string) {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = $1
			  AND table_name = $2
			  AND column_name = $3
		)
	`, schemaName, tableName, columnName).Scan(&exists)
	if err != nil {
		t.Fatalf("check tenant column existence: %v", err)
	}
	if !exists {
		t.Fatalf("expected tenant column %s.%s.%s to exist", schemaName, tableName, columnName)
	}
}

func assertUniqueIndexExistsOnColumns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string, columns ...string) {
	t.Helper()
	for _, column := range columns {
		exists, err := uniqueIndexOnColumnExists(ctx, pool, schemaName, tableName, column)
		if err != nil {
			t.Fatalf("check unique index for %s.%s(%s): %v", schemaName, tableName, column, err)
		}
		if !exists {
			t.Fatalf("expected unique index for %s.%s to include column %s", schemaName, tableName, column)
		}
	}
}

type sequenceIDGenerator struct {
	values []uuid.UUID
	index  int
}

func (g *sequenceIDGenerator) New() uuid.UUID {
	if g.index >= len(g.values) {
		panic("sequenceIDGenerator exhausted")
	}
	value := g.values[g.index]
	g.index++
	return value
}

type fixedIntegrationClock struct {
	now time.Time
}

func (c fixedIntegrationClock) Now() time.Time {
	return c.now
}
