package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/tenant"
	storepostgres "github.com/Kwasi-itc/marble-datamodel-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/marble-datamodel-service/internal/tenantdb/postgres"
)

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

	field, err := fieldService.Create(ctx, CreateFieldInput{
		TableID:  table.ID,
		Name:     "email",
		DataType: datamodel.DataTypeString,
		Nullable: false,
	})
	if err != nil {
		t.Fatalf("create field: %v", err)
	}

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
