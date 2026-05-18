package postgres

import (
	"context"
	"encoding/json"
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
)

func TestPostgresRepositoriesRoundTripAndAssembledRead(t *testing.T) {
	databaseURL := repositoryIntegrationDatabaseURL(t)
	ctx := context.Background()
	pool := repositoryIntegrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetRepositoryIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantRepo := NewTenantRepository(pool)
	tableRepo := NewTableRepository(pool)
	fieldRepo := NewFieldRepository(pool)
	linkRepo := NewLinkRepository(pool)
	pivotRepo := NewPivotRepository(pool)
	optionsRepo := NewTableOptionsRepository(pool)
	navigationOptionRepo := NewNavigationOptionRepository(pool)
	schemaChangeRepo := NewSchemaChangeRepository(pool)
	tenantSchemaMigrationRepo := NewTenantSchemaMigrationRepository(pool)
	indexJobRepo := NewIndexJobRepository(pool)
	readRepo := NewDataModelReadRepository(pool)

	now := time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)
	tenantID := uuid.MustParse("40000000-0000-0000-0000-000000000001")
	accountTableID := uuid.MustParse("40000000-0000-0000-0000-000000000002")
	transactionTableID := uuid.MustParse("40000000-0000-0000-0000-000000000003")
	accountObjectIDFieldID := uuid.MustParse("40000000-0000-0000-0000-000000000004")
	transactionObjectIDFieldID := uuid.MustParse("40000000-0000-0000-0000-000000000005")
	transactionAccountIDFieldID := uuid.MustParse("40000000-0000-0000-0000-000000000006")
	linkID := uuid.MustParse("40000000-0000-0000-0000-000000000007")
	pivotID := uuid.MustParse("40000000-0000-0000-0000-000000000008")
	optionsID := uuid.MustParse("40000000-0000-0000-0000-000000000009")
	changeID := uuid.MustParse("40000000-0000-0000-0000-000000000010")
	migrationID := uuid.MustParse("40000000-0000-0000-0000-000000000011")
	navigationOptionID := uuid.MustParse("40000000-0000-0000-0000-000000000012")
	indexJobID := uuid.MustParse("40000000-0000-0000-0000-000000000013")

	externalKey := "repo-tenant"
	record := tenant.New(tenantID, now, "Repository Tenant", &externalKey)
	if err := tenantRepo.Create(ctx, record); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := tenantRepo.UpdateStatus(ctx, tenantID, tenant.StatusActive); err != nil {
		t.Fatalf("update tenant status: %v", err)
	}
	reloadedTenant, err := tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if reloadedTenant.Status != tenant.StatusActive {
		t.Fatalf("expected active tenant, got %s", reloadedTenant.Status)
	}

	accountTable := datamodel.Table{
		ID:           accountTableID,
		TenantID:     tenantID,
		Name:         "accounts",
		Description:  "Customer accounts",
		Alias:        "Accounts",
		SemanticType: "party",
		CaptionField: "object_id",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	transactionTable := datamodel.Table{
		ID:          transactionTableID,
		TenantID:    tenantID,
		Name:        "transactions",
		Description: "Transaction records",
		CreatedAt:   now.Add(time.Minute),
		UpdatedAt:   now.Add(time.Minute),
	}
	if err := tableRepo.Create(ctx, accountTable); err != nil {
		t.Fatalf("create account table: %v", err)
	}
	if err := tableRepo.Create(ctx, transactionTable); err != nil {
		t.Fatalf("create transaction table: %v", err)
	}
	transactionTable.Description = "Updated transaction records"
	transactionTable.Alias = "Transactions"
	transactionTable.CaptionField = "account_id"
	transactionTable.UpdatedAt = now.Add(2 * time.Minute)
	if err := tableRepo.Update(ctx, transactionTable); err != nil {
		t.Fatalf("update transaction table: %v", err)
	}

	accountObjectIDField := datamodel.Field{
		ID:          accountObjectIDFieldID,
		TenantID:    tenantID,
		TableID:     accountTableID,
		Name:        "object_id",
		Description: "Account external identifier",
		DataType:    datamodel.DataTypeString,
		IsUnique:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	transactionObjectIDField := datamodel.Field{
		ID:          transactionObjectIDFieldID,
		TenantID:    tenantID,
		TableID:     transactionTableID,
		Name:        "object_id",
		Description: "Transaction external identifier",
		DataType:    datamodel.DataTypeString,
		IsUnique:    true,
		CreatedAt:   now.Add(time.Minute),
		UpdatedAt:   now.Add(time.Minute),
	}
	transactionAccountIDField := datamodel.Field{
		ID:          transactionAccountIDFieldID,
		TenantID:    tenantID,
		TableID:     transactionTableID,
		Name:        "account_id",
		Description: "Linked account id",
		DataType:    datamodel.DataTypeString,
		CreatedAt:   now.Add(2 * time.Minute),
		UpdatedAt:   now.Add(2 * time.Minute),
	}
	for _, field := range []datamodel.Field{accountObjectIDField, transactionObjectIDField, transactionAccountIDField} {
		if err := fieldRepo.Create(ctx, field); err != nil {
			t.Fatalf("create field %s: %v", field.Name, err)
		}
	}
	transactionAccountIDField.Description = "Updated linked account id"
	transactionAccountIDField.Nullable = true
	transactionAccountIDField.UpdatedAt = now.Add(3 * time.Minute)
	if err := fieldRepo.Update(ctx, transactionAccountIDField); err != nil {
		t.Fatalf("update field: %v", err)
	}

	link := datamodel.Link{
		ID:          linkID,
		TenantID:    tenantID,
		Name:        "account",
		ParentTable: accountTableID,
		ParentField: accountObjectIDFieldID,
		ChildTable:  transactionTableID,
		ChildField:  transactionAccountIDFieldID,
		CreatedAt:   now.Add(4 * time.Minute),
	}
	if err := linkRepo.Create(ctx, link); err != nil {
		t.Fatalf("create link: %v", err)
	}

	pivot := datamodel.Pivot{
		ID:          pivotID,
		TenantID:    tenantID,
		BaseTableID: transactionTableID,
		PathLinkIDs: []uuid.UUID{linkID},
		CreatedAt:   now.Add(5 * time.Minute),
	}
	if err := pivotRepo.Create(ctx, pivot); err != nil {
		t.Fatalf("create pivot: %v", err)
	}

	options := datamodel.TableOptions{
		ID:              optionsID,
		TenantID:        tenantID,
		TableID:         transactionTableID,
		DisplayedFields: []uuid.UUID{transactionAccountIDFieldID},
		FieldOrder:      []uuid.UUID{transactionAccountIDFieldID, transactionObjectIDFieldID},
		UpdatedAt:       now.Add(6 * time.Minute),
	}
	if err := optionsRepo.Upsert(ctx, options); err != nil {
		t.Fatalf("upsert table options: %v", err)
	}
	navigationOption := datamodel.NavigationOption{
		ID:                navigationOptionID,
		TenantID:          tenantID,
		SourceTableID:     accountTableID,
		SourceFieldID:     accountObjectIDFieldID,
		TargetTableID:     transactionTableID,
		FilterFieldID:     transactionAccountIDFieldID,
		OrderingFieldID:   transactionObjectIDFieldID,
		CreatedAt:         now.Add(6 * time.Minute),
	}
	if err := navigationOptionRepo.Create(ctx, navigationOption); err != nil {
		t.Fatalf("create navigation option: %v", err)
	}
	indexJob := datamodel.IndexJob{
		ID:                   indexJobID,
		TenantID:             tenantID,
		TableID:              &transactionTableID,
		TableName:            "transactions",
		IndexType:            datamodel.IndexJobTypeNavigation,
		Columns:              []string{"account_id", "object_id"},
		Status:               datamodel.IndexJobStatusPending,
		RequestedByOperation: "repository_test",
		RequestedAt:          now.Add(6 * time.Minute),
		DedupeKey:            "repo-test-navigation-index",
	}
	if err := indexJobRepo.Create(ctx, indexJob); err != nil {
		t.Fatalf("create index job: %v", err)
	}

	details, _ := json.Marshal(map[string]any{"ok": true})
	change := datamodel.SchemaChange{
		ID:           changeID,
		TenantID:     tenantID,
		Operation:    "repository_check",
		ResourceType: "table",
		ResourceID:   transactionTableID,
		Status:       "applied",
		Details:      details,
		CreatedAt:    now.Add(7 * time.Minute),
	}
	if err := schemaChangeRepo.Create(ctx, change); err != nil {
		t.Fatalf("create schema change: %v", err)
	}

	migration := datamodel.TenantSchemaMigration{
		ID:        migrationID,
		TenantID:  tenantID,
		Version:   "repository_check:table",
		AppliedAt: now.Add(8 * time.Minute),
	}
	if err := tenantSchemaMigrationRepo.Create(ctx, migration); err != nil {
		t.Fatalf("create tenant schema migration: %v", err)
	}

	tenantList, err := tenantRepo.List(ctx)
	if err != nil {
		t.Fatalf("list tenants: %v", err)
	}
	if len(tenantList) != 1 || tenantList[0].ID != tenantID {
		t.Fatalf("unexpected tenant list: %v", tenantList)
	}

	tableList, err := tableRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(tableList) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(tableList))
	}

	fieldList, err := fieldRepo.ListByTable(ctx, transactionTableID)
	if err != nil {
		t.Fatalf("list fields: %v", err)
	}
	if len(fieldList) != 2 {
		t.Fatalf("expected 2 transaction fields, got %d", len(fieldList))
	}
	if got := findFieldByName(fieldList, "account_id"); got.Description != "Updated linked account id" || !got.Nullable {
		t.Fatalf("unexpected updated field: %+v", got)
	}

	links, err := linkRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 1 || links[0].ID != linkID {
		t.Fatalf("unexpected links: %v", links)
	}

	pivots, err := pivotRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list pivots: %v", err)
	}
	if len(pivots) != 1 || pivots[0].ID != pivotID {
		t.Fatalf("unexpected pivots: %v", pivots)
	}

	storedOptions, err := optionsRepo.GetByTableID(ctx, transactionTableID)
	if err != nil {
		t.Fatalf("get table options: %v", err)
	}
	if storedOptions == nil || !slices.Equal(storedOptions.FieldOrder, options.FieldOrder) {
		t.Fatalf("unexpected table options: %#v", storedOptions)
	}
	storedNavigationOptions, err := navigationOptionRepo.ListBySourceTable(ctx, accountTableID)
	if err != nil {
		t.Fatalf("list navigation options: %v", err)
	}
	if len(storedNavigationOptions) != 1 {
		t.Fatalf("expected 1 navigation option, got %d", len(storedNavigationOptions))
	}
	if storedNavigationOptions[0].SourceTableName != "accounts" || storedNavigationOptions[0].TargetTableName != "transactions" {
		t.Fatalf("unexpected navigation option names: %+v", storedNavigationOptions[0])
	}
	claimedJob, err := indexJobRepo.ClaimNext(ctx, now.Add(7*time.Minute), 3)
	if err != nil {
		t.Fatalf("claim index job: %v", err)
	}
	if claimedJob.ID != indexJobID || claimedJob.Status != datamodel.IndexJobStatusRunning {
		t.Fatalf("unexpected claimed job: %+v", claimedJob)
	}
	if err := indexJobRepo.MarkFailed(ctx, indexJobID, "repo failure", now.Add(8*time.Minute)); err != nil {
		t.Fatalf("mark index job failed: %v", err)
	}
	failedJob, err := indexJobRepo.GetByID(ctx, indexJobID)
	if err != nil {
		t.Fatalf("get failed index job: %v", err)
	}
	if failedJob.ErrorMessage == nil || *failedJob.ErrorMessage != "repo failure" {
		t.Fatalf("unexpected failed job message: %+v", failedJob)
	}
	if err := indexJobRepo.Retry(ctx, indexJobID, now.Add(9*time.Minute)); err != nil {
		t.Fatalf("retry index job: %v", err)
	}
	retriedJob, err := indexJobRepo.GetByID(ctx, indexJobID)
	if err != nil {
		t.Fatalf("get retried index job: %v", err)
	}
	if retriedJob.Status != datamodel.IndexJobStatusPending || retriedJob.ScheduledAt == nil {
		t.Fatalf("unexpected retried job: %+v", retriedJob)
	}

	changes, err := schemaChangeRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list schema changes: %v", err)
	}
	if len(changes) != 1 || changes[0].Operation != "repository_check" {
		t.Fatalf("unexpected schema changes: %v", changes)
	}

	migrations, err := tenantSchemaMigrationRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list tenant schema migrations: %v", err)
	}
	if len(migrations) != 1 || migrations[0].Version != "repository_check:table" {
		t.Fatalf("unexpected tenant schema migrations: %v", migrations)
	}

	model, err := readRepo.GetAssembledDataModel(ctx, tenantID)
	if err != nil {
		t.Fatalf("assembled data model: %v", err)
	}
	txTable, ok := model.Tables["transactions"]
	if !ok {
		t.Fatal("expected transactions table in assembled model")
	}
	if txTable.Description != "Updated transaction records" || txTable.CaptionField != "account_id" {
		t.Fatalf("unexpected assembled table: %+v", txTable)
	}
	if _, ok := txTable.Fields["account_id"]; !ok {
		t.Fatal("expected account_id in assembled fields")
	}
	if linkRef, ok := txTable.LinksToSingle["account"]; !ok || linkRef.ParentTableName != "accounts" {
		t.Fatalf("unexpected assembled link: %+v", txTable.LinksToSingle)
	}
	if txTable.Options == nil || !slices.Equal(txTable.Options.FieldOrder, options.FieldOrder) {
		t.Fatalf("unexpected assembled options: %#v", txTable.Options)
	}
	if len(model.Tables["accounts"].NavigationOptions) != 1 || model.Tables["accounts"].NavigationOptions[0].TargetTableName != "transactions" {
		t.Fatalf("unexpected assembled navigation options: %+v", model.Tables["accounts"].NavigationOptions)
	}
	if len(model.Pivots) != 1 || !slices.Equal(model.Pivots[0].PathLinks, []string{"account"}) {
		t.Fatalf("unexpected assembled pivots: %v", model.Pivots)
	}

	if err := pivotRepo.Delete(ctx, pivotID); err != nil {
		t.Fatalf("delete pivot: %v", err)
	}
	if err := linkRepo.Delete(ctx, linkID); err != nil {
		t.Fatalf("delete link: %v", err)
	}
	if err := fieldRepo.Delete(ctx, transactionAccountIDFieldID); err != nil {
		t.Fatalf("delete field: %v", err)
	}
	if err := tableRepo.Delete(ctx, transactionTableID); err != nil {
		t.Fatalf("delete table: %v", err)
	}

	tablesAfterDelete, err := tableRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("list tables after delete: %v", err)
	}
	if len(tablesAfterDelete) != 1 || tablesAfterDelete[0].ID != accountTableID {
		t.Fatalf("unexpected tables after delete: %v", tablesAfterDelete)
	}
}

func repositoryIntegrationDatabaseURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("DATA_MODEL_TEST_DATABASE_URL"); url != "" {
		return url
	}
	t.Skip("set DATA_MODEL_TEST_DATABASE_URL to run PostgreSQL integration tests")
	return ""
}

func repositoryIntegrationPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect integration pool: %v", err)
	}
	return pool
}

func resetRepositoryIntegrationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseURL string) {
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

	runRepositoryMetadataMigrations(t, databaseURL)
}

func runRepositoryMetadataMigrations(t *testing.T, databaseURL string) {
	t.Helper()

	_, fileName, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}
	rootDir := filepath.Clean(filepath.Join(filepath.Dir(fileName), "..", "..", ".."))
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

func findFieldByName(fields []datamodel.Field, name string) datamodel.Field {
	for _, field := range fields {
		if field.Name == name {
			return field
		}
	}
	return datamodel.Field{}
}
