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
	fieldEnumValueRepo := storepostgres.NewFieldEnumValueRepository(pool)
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
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, fieldEnumValueRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
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

func TestIntegrationEnumValuesPersistAndAppearInAssembledModel(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	ctx := context.Background()
	pool := integrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantRepo := storepostgres.NewTenantRepository(pool)
	tableRepo := storepostgres.NewTableRepository(pool)
	fieldRepo := storepostgres.NewFieldRepository(pool)
	fieldEnumValueRepo := storepostgres.NewFieldEnumValueRepository(pool)
	linkRepo := storepostgres.NewLinkRepository(pool)
	pivotRepo := storepostgres.NewPivotRepository(pool)
	readRepo := storepostgres.NewDataModelReadRepository(pool)
	schemaChanges := storepostgres.NewSchemaChangeRepository(pool)
	schemaManager := tenantdbpostgres.NewSchemaManager(pool)
	txManager := storepostgres.NewTransactionManager(pool)

	idGen := &sequenceIDGenerator{values: integrationUUIDSequence(20)}
	clock := fixedIntegrationClock{now: time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)}

	tenantService := NewTenantService(tenantRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	tableService := NewTableService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, fieldEnumValueRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	enumValueService := NewFieldEnumValueService(fieldRepo, fieldEnumValueRepo, schemaChanges, txManager, idGen, clock)
	readService := NewDataModelReadService(readRepo)

	record, err := tenantService.Create(ctx, tenant.CreateInput{Name: "Enum Tenant"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	record, err = tenantService.Provision(ctx, record.ID)
	if err != nil {
		t.Fatalf("provision tenant: %v", err)
	}

	table, err := tableService.Create(ctx, CreateTableInput{
		TenantID:    record.ID,
		Name:        "cases",
		Description: "Case records",
	})
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	field, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:     table.ID,
		Name:        "status",
		Description: "Case status",
		DataType:    datamodel.DataTypeString,
		Nullable:    false,
		IsEnum:      true,
	})
	if err != nil {
		t.Fatalf("create enum field: %v", err)
	}

	if _, err := enumValueService.Create(ctx, CreateFieldEnumValueInput{
		FieldID:   field.ID,
		Value:     "pending",
		Label:     "Pending",
		SortOrder: 10,
	}); err != nil {
		t.Fatalf("create first enum value: %v", err)
	}
	if _, err := enumValueService.Create(ctx, CreateFieldEnumValueInput{
		FieldID:   field.ID,
		Value:     "approved",
		Label:     "Approved",
		SortOrder: 20,
	}); err != nil {
		t.Fatalf("create second enum value: %v", err)
	}

	values, err := enumValueService.List(ctx, field.ID)
	if err != nil {
		t.Fatalf("list enum values: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 enum values, got %d", len(values))
	}
	if values[0].Value != "pending" || values[1].Value != "approved" {
		t.Fatalf("unexpected enum values ordering: %#v", values)
	}

	model, err := readService.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("read assembled data model: %v", err)
	}
	statusField := model.Tables["cases"].Fields["status"]
	if len(statusField.EnumValues) != 2 {
		t.Fatalf("expected assembled field to contain 2 enum values, got %d", len(statusField.EnumValues))
	}
	if statusField.EnumValues[0].Label != "Pending" || statusField.EnumValues[1].Label != "Approved" {
		t.Fatalf("unexpected assembled enum values: %#v", statusField.EnumValues)
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
	fieldEnumValueRepo := storepostgres.NewFieldEnumValueRepository(pool)
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, fieldEnumValueRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, &idGen, clock)

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

func TestIntegrationDeleteDryRunsReportInternalConflicts(t *testing.T) {
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
	schemaManager := tenantdbpostgres.NewSchemaManager(pool)
	txManager := storepostgres.NewTransactionManager(pool)

	idGen := &sequenceIDGenerator{values: integrationUUIDSequence(50)}
	clock := fixedIntegrationClock{now: time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)}

	tenantService := NewTenantService(tenantRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	tableService := NewTableService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	fieldEnumValueRepo := storepostgres.NewFieldEnumValueRepository(pool)
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, fieldEnumValueRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	linkService := NewLinkService(tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, txManager, idGen, clock)
	pivotService := NewPivotService(tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, txManager, idGen, clock)

	record, err := tenantService.Create(ctx, tenant.CreateInput{Name: "Conflict Tenant"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	record, err = tenantService.Provision(ctx, record.ID)
	if err != nil {
		t.Fatalf("provision tenant: %v", err)
	}

	accounts, err := tableService.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "accounts"})
	if err != nil {
		t.Fatalf("create accounts table: %v", err)
	}
	transactions, err := tableService.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "transactions"})
	if err != nil {
		t.Fatalf("create transactions table: %v", err)
	}

	transactionsAccountID, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:  transactions.ID,
		Name:     "account_id",
		DataType: datamodel.DataTypeString,
	})
	if err != nil {
		t.Fatalf("create transactions.account_id: %v", err)
	}

	accountFields, err := fieldRepo.ListByTable(ctx, accounts.ID)
	if err != nil {
		t.Fatalf("list account fields: %v", err)
	}
	accountsObjectID := findFieldByName(t, accountFields, "object_id")

	link, err := linkService.Create(ctx, CreateLinkInput{
		TenantID:    record.ID,
		Name:        "account",
		ParentTable: accounts.ID,
		ParentField: accountsObjectID.ID,
		ChildTable:  transactions.ID,
		ChildField:  transactionsAccountID.ID,
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	fieldPivot, err := pivotService.Create(ctx, CreatePivotInput{
		TenantID:    record.ID,
		BaseTableID: transactions.ID,
		FieldID:     &transactionsAccountID.ID,
	})
	if err != nil {
		t.Fatalf("create field-based pivot: %v", err)
	}
	pathPivot, err := pivotService.Create(ctx, CreatePivotInput{
		TenantID:    record.ID,
		BaseTableID: transactions.ID,
		PathLinkIDs: []uuid.UUID{link.ID},
	})
	if err != nil {
		t.Fatalf("create path-based pivot: %v", err)
	}

	fieldReport, err := fieldService.Delete(ctx, transactionsAccountID.ID, true)
	if err == nil {
		t.Fatal("expected field dry-run delete conflict")
	}
	if fieldReport.Performed {
		t.Fatal("expected field dry-run not to perform delete")
	}
	if !slices.Contains(fieldReport.Conflicts.Links, link.ID) {
		t.Fatalf("expected field conflict to include link %s, got %v", link.ID, fieldReport.Conflicts.Links)
	}
	if !slices.Contains(fieldReport.Conflicts.Pivots, fieldPivot.ID) {
		t.Fatalf("expected field conflict to include pivot %s, got %v", fieldPivot.ID, fieldReport.Conflicts.Pivots)
	}

	linkReport, err := linkService.Delete(ctx, link.ID, true)
	if err == nil {
		t.Fatal("expected link dry-run delete conflict")
	}
	if linkReport.Performed {
		t.Fatal("expected link dry-run not to perform delete")
	}
	if !slices.Contains(linkReport.Conflicts.Pivots, pathPivot.ID) {
		t.Fatalf("expected link conflict to include pivot %s, got %v", pathPivot.ID, linkReport.Conflicts.Pivots)
	}

	tableReport, err := tableService.Delete(ctx, transactions.ID, true)
	if err == nil {
		t.Fatal("expected table dry-run delete conflict")
	}
	if tableReport.Performed {
		t.Fatal("expected table dry-run not to perform delete")
	}
	if !slices.Contains(tableReport.Conflicts.Links, link.ID) {
		t.Fatalf("expected table conflict to include link %s, got %v", link.ID, tableReport.Conflicts.Links)
	}
	if !slices.Contains(tableReport.Conflicts.Pivots, fieldPivot.ID) || !slices.Contains(tableReport.Conflicts.Pivots, pathPivot.ID) {
		t.Fatalf("expected table conflict to include pivots %s and %s, got %v", fieldPivot.ID, pathPivot.ID, tableReport.Conflicts.Pivots)
	}

	assertTenantColumnExists(t, ctx, pool, record.SchemaName, transactions.Name, transactionsAccountID.Name)
}

func TestIntegrationDeleteOperationsRemoveMetadataAndTenantDDL(t *testing.T) {
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

	idGen := &sequenceIDGenerator{values: integrationUUIDSequence(60)}
	clock := fixedIntegrationClock{now: time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)}

	tenantService := NewTenantService(tenantRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	tableService := NewTableService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	fieldEnumValueRepo := storepostgres.NewFieldEnumValueRepository(pool)
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, fieldEnumValueRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, clock)
	linkService := NewLinkService(tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, txManager, idGen, clock)
	pivotService := NewPivotService(tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, txManager, idGen, clock)

	record, err := tenantService.Create(ctx, tenant.CreateInput{Name: "Delete Tenant"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	record, err = tenantService.Provision(ctx, record.ID)
	if err != nil {
		t.Fatalf("provision tenant: %v", err)
	}

	accounts, err := tableService.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "accounts"})
	if err != nil {
		t.Fatalf("create accounts table: %v", err)
	}
	transactions, err := tableService.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "transactions"})
	if err != nil {
		t.Fatalf("create transactions table: %v", err)
	}
	casesTable, err := tableService.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "cases"})
	if err != nil {
		t.Fatalf("create cases table: %v", err)
	}

	transactionsAccountID, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:  transactions.ID,
		Name:     "account_id",
		DataType: datamodel.DataTypeString,
	})
	if err != nil {
		t.Fatalf("create transactions.account_id: %v", err)
	}
	casesEmail, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:   casesTable.ID,
		Name:      "email",
		DataType:  datamodel.DataTypeString,
		IsUnique:  true,
		Nullable:  false,
	})
	if err != nil {
		t.Fatalf("create cases.email: %v", err)
	}

	accountFields, err := fieldRepo.ListByTable(ctx, accounts.ID)
	if err != nil {
		t.Fatalf("list account fields: %v", err)
	}
	accountsObjectID := findFieldByName(t, accountFields, "object_id")

	link, err := linkService.Create(ctx, CreateLinkInput{
		TenantID:    record.ID,
		Name:        "account",
		ParentTable: accounts.ID,
		ParentField: accountsObjectID.ID,
		ChildTable:  transactions.ID,
		ChildField:  transactionsAccountID.ID,
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	pathPivot, err := pivotService.Create(ctx, CreatePivotInput{
		TenantID:    record.ID,
		BaseTableID: transactions.ID,
		PathLinkIDs: []uuid.UUID{link.ID},
	})
	if err != nil {
		t.Fatalf("create path pivot: %v", err)
	}

	pivotDeleteReport, err := pivotService.Delete(ctx, pathPivot.ID, false)
	if err != nil {
		t.Fatalf("delete pivot: %v", err)
	}
	if !pivotDeleteReport.Performed {
		t.Fatal("expected pivot delete to be performed")
	}
	if _, err := pivotRepo.GetByID(ctx, pathPivot.ID); err == nil {
		t.Fatal("expected pivot metadata to be deleted")
	}

	linkDeleteReport, err := linkService.Delete(ctx, link.ID, false)
	if err != nil {
		t.Fatalf("delete link: %v", err)
	}
	if !linkDeleteReport.Performed {
		t.Fatal("expected link delete to be performed")
	}
	if _, err := linkRepo.GetByID(ctx, link.ID); err == nil {
		t.Fatal("expected link metadata to be deleted")
	}

	fieldDeleteReport, err := fieldService.Delete(ctx, casesEmail.ID, false)
	if err != nil {
		t.Fatalf("delete field: %v", err)
	}
	if !fieldDeleteReport.Performed {
		t.Fatal("expected field delete to be performed")
	}
	if _, err := fieldRepo.GetByID(ctx, casesEmail.ID); err == nil {
		t.Fatal("expected field metadata to be deleted")
	}
	assertTenantColumnAbsent(t, ctx, pool, record.SchemaName, casesTable.Name, casesEmail.Name)
	exists, err := uniqueIndexOnColumnExists(ctx, pool, record.SchemaName, casesTable.Name, casesEmail.Name)
	if err != nil {
		t.Fatalf("check deleted field unique index: %v", err)
	}
	if exists {
		t.Fatal("expected unique index for deleted field to be removed")
	}

	tableDeleteReport, err := tableService.Delete(ctx, casesTable.ID, false)
	if err != nil {
		t.Fatalf("delete table: %v", err)
	}
	if !tableDeleteReport.Performed {
		t.Fatal("expected table delete to be performed")
	}
	if _, err := tableRepo.GetByID(ctx, casesTable.ID); err == nil {
		t.Fatal("expected table metadata to be deleted")
	}
	assertTenantTableAbsent(t, ctx, pool, record.SchemaName, casesTable.Name)

	changes, err := schemaChanges.ListByTenant(ctx, record.ID)
	if err != nil {
		t.Fatalf("list schema changes: %v", err)
	}
	assertSchemaChangeOperations(t, changes, "delete_pivot", "delete_link", "delete_field", "delete_table")

	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "delete_pivot:pivot")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "delete_link:link")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "delete_field:field")
	assertTenantSchemaMigrationVersionExists(t, ctx, tenantSchemaMigrations, record.ID, "delete_table:table")
}

func TestIntegrationCreateTableRollsBackWhenTenantSchemaMissing(t *testing.T) {
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
	schemaManager := tenantdbpostgres.NewSchemaManager(pool)
	txManager := storepostgres.NewTransactionManager(pool)

	now := time.Date(2026, 5, 13, 16, 0, 0, 0, time.UTC)
	idGen := &sequenceIDGenerator{values: integrationUUIDSequence(20)}
	tableService := NewTableService(tenantRepo, tableRepo, fieldRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, fixedIntegrationClock{now: now})

	record := tenant.Tenant{
		ID:         uuid.MustParse("30000000-0000-0000-0000-000000000001"),
		Name:       "Broken Tenant",
		SchemaName: tenant.SchemaNameFor(uuid.MustParse("30000000-0000-0000-0000-000000000001")),
		Status:     tenant.StatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := tenantRepo.Create(ctx, record); err != nil {
		t.Fatalf("seed active tenant: %v", err)
	}

	_, err := tableService.Create(ctx, CreateTableInput{
		TenantID: record.ID,
		Name:     "transactions",
	})
	if err == nil {
		t.Fatal("expected create table to fail when tenant schema is missing")
	}

	tables, err := tableRepo.ListByTenant(ctx, record.ID)
	if err != nil {
		t.Fatalf("list tables after failed create: %v", err)
	}
	if len(tables) != 0 {
		t.Fatalf("expected no table metadata after rollback, got %v", tables)
	}

	changes, err := schemaChanges.ListByTenant(ctx, record.ID)
	if err != nil {
		t.Fatalf("list schema changes after failed create: %v", err)
	}
	for _, change := range changes {
		if change.Operation == "create_table" {
			t.Fatalf("expected no persisted create_table schema change after rollback, got %v", changes)
		}
	}
}

func TestIntegrationCreateFieldRollsBackWhenPhysicalTableMissing(t *testing.T) {
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
	schemaManager := tenantdbpostgres.NewSchemaManager(pool)
	txManager := storepostgres.NewTransactionManager(pool)

	now := time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC)
	idGen := &sequenceIDGenerator{values: integrationUUIDSequence(30)}
	fieldEnumValueRepo := storepostgres.NewFieldEnumValueRepository(pool)
	fieldService := NewFieldService(tenantRepo, tableRepo, fieldRepo, fieldEnumValueRepo, linkRepo, pivotRepo, schemaChanges, schemaManager, txManager, idGen, fixedIntegrationClock{now: now})

	tenantID := uuid.MustParse("31000000-0000-0000-0000-000000000001")
	record := tenant.Tenant{
		ID:         tenantID,
		Name:       "Missing Table Tenant",
		SchemaName: tenant.SchemaNameFor(tenantID),
		Status:     tenant.StatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := tenantRepo.Create(ctx, record); err != nil {
		t.Fatalf("seed active tenant: %v", err)
	}
	if err := schemaManager.ProvisionTenantSchema(ctx, record); err != nil {
		t.Fatalf("provision tenant schema directly: %v", err)
	}

	tableID := uuid.MustParse("31000000-0000-0000-0000-000000000002")
	table := datamodel.Table{
		ID:          tableID,
		TenantID:    tenantID,
		Name:        "transactions",
		Description: "metadata only table",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := tableRepo.Create(ctx, table); err != nil {
		t.Fatalf("seed metadata table: %v", err)
	}

	_, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:   tableID,
		Name:      "amount",
		DataType:  datamodel.DataTypeFloat,
		Nullable:  false,
		IsUnique:  false,
		IsEnum:    false,
	})
	if err == nil {
		t.Fatal("expected create field to fail when physical tenant table is missing")
	}

	fields, err := fieldRepo.ListByTable(ctx, tableID)
	if err != nil {
		t.Fatalf("list fields after failed create: %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("expected no field metadata after rollback, got %v", fields)
	}

	changes, err := schemaChanges.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list schema changes after failed field create: %v", err)
	}
	for _, change := range changes {
		if change.Operation == "create_field" {
			t.Fatalf("expected no persisted create_field schema change after rollback, got %v", changes)
		}
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

func assertTenantColumnAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName, columnName string) {
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
		t.Fatalf("check tenant column absence: %v", err)
	}
	if exists {
		t.Fatalf("expected tenant column %s.%s.%s to be absent", schemaName, tableName, columnName)
	}
}

func assertTenantTableAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) {
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
		t.Fatalf("check tenant table absence: %v", err)
	}
	if exists {
		t.Fatalf("expected tenant table %s.%s to be absent", schemaName, tableName)
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
