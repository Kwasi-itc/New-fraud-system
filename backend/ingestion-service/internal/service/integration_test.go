package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
)

func TestIntegrationSingleAndPatchIngestWritesTenantRecordAndMetadata(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	ctx := context.Background()
	pool := integrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	schemaName := tenantSchemaName(tenantID)
	createTenantTable(t, ctx, pool, schemaName, "transactions")

	idGenerator := &fixedIDGenerator{next: integrationUUIDSequence(20)}
	service := NewIngestService(
		stubPublishedModelReader{
			model: publishedTransactionsModel(tenantID),
		},
		storepostgres.NewTransactionManager(pool),
		idGenerator,
		fixedClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)},
	)

	createResult, validationErrors, err := service.Ingest(ctx, IngestInput{
		TenantID:   tenantID,
		ObjectType: "transactions",
		Mode:       ingestion.ModeCreate,
		Payload: map[string]any{
			"object_id": "txn-1",
			"amount":    10.5,
			"status":    "pending",
		},
	})
	if err != nil {
		t.Fatalf("create ingest returned error: %v", err)
	}
	if len(validationErrors) != 0 {
		t.Fatalf("expected no validation errors, got %+v", validationErrors)
	}
	if createResult.Action != "created" {
		t.Fatalf("expected created action, got %+v", createResult)
	}

	patchResult, validationErrors, err := service.Ingest(ctx, IngestInput{
		TenantID:   tenantID,
		ObjectType: "transactions",
		Mode:       ingestion.ModePatch,
		Payload: map[string]any{
			"object_id": "txn-1",
			"status":    "approved",
		},
	})
	if err != nil {
		t.Fatalf("patch ingest returned error: %v", err)
	}
	if len(validationErrors) != 0 {
		t.Fatalf("expected no validation errors on patch, got %+v", validationErrors)
	}
	if patchResult.Action != "updated" {
		t.Fatalf("expected updated action, got %+v", patchResult)
	}

	assertTenantRowValues(t, ctx, pool, schemaName, "transactions", "txn-1", "approved", 10.5)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.ingestion_audit", 2)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.outbox_events", 2)
}

func TestIntegrationSingleIngestReplaysStoredIdempotentResponse(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	ctx := context.Background()
	pool := integrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantID := uuid.MustParse("12121212-1212-1212-1212-121212121212")
	schemaName := tenantSchemaName(tenantID)
	createTenantTable(t, ctx, pool, schemaName, "transactions")

	idGenerator := &fixedIDGenerator{next: integrationUUIDSequence(20)}
	service := NewIngestService(
		stubPublishedModelReader{
			model: publishedTransactionsModel(tenantID),
		},
		storepostgres.NewTransactionManager(pool),
		idGenerator,
		fixedClock{now: time.Date(2026, 5, 20, 12, 30, 0, 0, time.UTC)},
	)

	key := "idem-txn-1"
	input := IngestInput{
		TenantID:       tenantID,
		ObjectType:     "transactions",
		Mode:           ingestion.ModeCreate,
		IdempotencyKey: &key,
		Payload: map[string]any{
			"object_id": "txn-1",
			"amount":    10.5,
			"status":    "pending",
		},
	}

	first, validationErrors, err := service.Ingest(ctx, input)
	if err != nil {
		t.Fatalf("first ingest returned error: %v", err)
	}
	if len(validationErrors) != 0 {
		t.Fatalf("expected no validation errors, got %+v", validationErrors)
	}
	if first.Replayed {
		t.Fatalf("first result should not be replayed: %+v", first)
	}

	second, validationErrors, err := service.Ingest(ctx, input)
	if err != nil {
		t.Fatalf("second ingest returned error: %v", err)
	}
	if len(validationErrors) != 0 {
		t.Fatalf("expected no validation errors on replay, got %+v", validationErrors)
	}
	if !second.Replayed {
		t.Fatalf("expected replayed result, got %+v", second)
	}
	if second.ObjectID != first.ObjectID || second.Action != first.Action || second.RevisionID != first.RevisionID {
		t.Fatalf("expected replayed result to match first result, first=%+v second=%+v", first, second)
	}

	assertTenantRowValues(t, ctx, pool, schemaName, "transactions", "txn-1", "pending", 10.5)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.ingestion_audit", 1)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.outbox_events", 1)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.idempotency_keys", 1)
}

func TestIntegrationCSVUploadServiceProcessesUploadedLog(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	ctx := context.Background()
	pool := integrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	schemaName := tenantSchemaName(tenantID)
	createTenantTable(t, ctx, pool, schemaName, "transactions")

	idGenerator := &fixedIDGenerator{next: integrationUUIDSequence(20)}
	clock := fixedClock{now: time.Date(2026, 5, 20, 13, 0, 0, 0, time.UTC)}
	ingestService := NewIngestService(
		stubPublishedModelReader{
			model: publishedTransactionsModel(tenantID),
		},
		storepostgres.NewTransactionManager(pool),
		idGenerator,
		clock,
	)
	uploadService := NewUploadLogService(
		storepostgres.NewUploadLogRepository(pool),
		ingestService,
		idGenerator,
		clock,
		3,
	)

	log, err := uploadService.Create(
		ctx,
		tenantID,
		"transactions",
		ingestion.ModeCreate,
		"transactions.csv",
		"text/csv",
		[]byte("object_id,amount,status\ncsv-1,17.25,pending\ncsv-2,18.00,approved\n"),
	)
	if err != nil {
		t.Fatalf("create upload log: %v", err)
	}
	if log.Status != ingestion.UploadLogStatusUploaded {
		t.Fatalf("expected uploaded status, got %+v", log)
	}

	processed, err := uploadService.ProcessNextUploaded(ctx)
	if err != nil {
		t.Fatalf("process next uploaded log: %v", err)
	}
	if !processed {
		t.Fatal("expected one uploaded log to be processed")
	}

	reloaded, err := uploadService.Get(ctx, uuid.MustParse(log.ID))
	if err != nil {
		t.Fatalf("reload upload log: %v", err)
	}
	if reloaded.Status != ingestion.UploadLogStatusCompleted {
		t.Fatalf("expected completed upload log, got %+v", reloaded)
	}
	if reloaded.SuccessfulRows != 2 || reloaded.FailedRows != 0 {
		t.Fatalf("unexpected upload log counters: %+v", reloaded)
	}

	assertTenantRowValues(t, ctx, pool, schemaName, "transactions", "csv-1", "pending", 17.25)
	assertTenantRowValues(t, ctx, pool, schemaName, "transactions", "csv-2", "approved", 18.0)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.ingestion_audit", 2)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.outbox_events", 3)
}

func TestIntegrationCSVUploadRetriesBeforeTerminalFailure(t *testing.T) {
	databaseURL := integrationDatabaseURL(t)
	ctx := context.Background()
	pool := integrationPool(t, ctx, databaseURL)
	defer pool.Close()

	resetIntegrationDatabase(t, ctx, pool, databaseURL)

	tenantID := uuid.MustParse("23232323-2323-2323-2323-232323232323")
	schemaName := tenantSchemaName(tenantID)
	createTenantTable(t, ctx, pool, schemaName, "transactions")

	idGenerator := &fixedIDGenerator{next: integrationUUIDSequence(20)}
	clock := fixedClock{now: time.Date(2026, 5, 20, 13, 30, 0, 0, time.UTC)}
	ingestService := NewIngestService(
		stubPublishedModelReader{
			model: publishedTransactionsModel(tenantID),
		},
		storepostgres.NewTransactionManager(pool),
		idGenerator,
		clock,
	)
	uploadService := NewUploadLogService(
		storepostgres.NewUploadLogRepository(pool),
		ingestService,
		idGenerator,
		clock,
		2,
	)

	log, err := uploadService.Create(
		ctx,
		tenantID,
		"transactions",
		ingestion.ModeCreate,
		"broken.csv",
		"text/csv",
		[]byte("\"object_id\",\"amount\",\"status\"\n\"broken"),
	)
	if err != nil {
		t.Fatalf("create upload log: %v", err)
	}

	processed, err := uploadService.ProcessNextUploaded(ctx)
	if err != nil {
		t.Fatalf("first process next uploaded log: %v", err)
	}
	if !processed {
		t.Fatal("expected first processing attempt")
	}

	reloaded, err := uploadService.Get(ctx, uuid.MustParse(log.ID))
	if err != nil {
		t.Fatalf("reload upload log after first attempt: %v", err)
	}
	if reloaded.Status != ingestion.UploadLogStatusUploaded {
		t.Fatalf("expected upload log to be requeued after first failure, got %+v", reloaded)
	}
	if reloaded.AttemptCount != 1 {
		t.Fatalf("expected attempt count 1 after first failure, got %+v", reloaded)
	}
	if reloaded.CompletedAt != nil {
		t.Fatalf("expected no completion timestamp while requeued, got %+v", reloaded)
	}

	processed, err = uploadService.ProcessNextUploaded(ctx)
	if err != nil {
		t.Fatalf("second process next uploaded log: %v", err)
	}
	if !processed {
		t.Fatal("expected second processing attempt")
	}

	reloaded, err = uploadService.Get(ctx, uuid.MustParse(log.ID))
	if err != nil {
		t.Fatalf("reload upload log after second attempt: %v", err)
	}
	if reloaded.Status != ingestion.UploadLogStatusFailed {
		t.Fatalf("expected upload log to fail terminally on max attempts, got %+v", reloaded)
	}
	if reloaded.AttemptCount != 2 {
		t.Fatalf("expected attempt count 2 after terminal failure, got %+v", reloaded)
	}
	if reloaded.CompletedAt == nil {
		t.Fatalf("expected completion timestamp on terminal failure, got %+v", reloaded)
	}
	if reloaded.ErrorMessage == nil || *reloaded.ErrorMessage == "" {
		t.Fatalf("expected stored error message on terminal failure, got %+v", reloaded)
	}

	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.ingestion_audit", 0)
	assertCoreIngestionCount(t, ctx, pool, "core_ingestion.outbox_events", 0)
}

type stubPublishedModelReader struct {
	model ingestion.PublishedDataModel
}

func (s stubPublishedModelReader) GetPublishedDataModel(context.Context, uuid.UUID) (ingestion.PublishedDataModel, error) {
	return s.model, nil
}

type fixedIDGenerator struct {
	next []uuid.UUID
}

func (g *fixedIDGenerator) New() uuid.UUID {
	if len(g.next) == 0 {
		panic("fixedIDGenerator exhausted")
	}
	value := g.next[0]
	g.next = g.next[1:]
	return value
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func publishedTransactionsModel(tenantID uuid.UUID) ingestion.PublishedDataModel {
	return ingestion.PublishedDataModel{
		TenantID:            tenantID,
		RevisionID:          "rev_transactions_v1",
		TenantStatus:        "active",
		Writable:            true,
		RecordLookupField:   "object_id",
		PartialUpdates:      true,
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {
				ID:           uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				Name:         "transactions",
				Description:  "Transaction records",
				CaptionField: "object_id",
				Fields: map[string]ingestion.FieldSchema{
					"amount": {
						ID:       uuid.MustParse("44444444-4444-4444-4444-444444444444"),
						Name:     "amount",
						DataType: "float",
						Nullable: false,
					},
					"status": {
						ID:       uuid.MustParse("55555555-5555-5555-5555-555555555555"),
						Name:     "status",
						DataType: "string",
						Nullable: false,
						IsEnum:   true,
						EnumValues: []ingestion.EnumValue{
							{ID: uuid.MustParse("66666666-6666-6666-6666-666666666666"), Value: "pending", Label: "Pending"},
							{ID: uuid.MustParse("77777777-7777-7777-7777-777777777777"), Value: "approved", Label: "Approved"},
						},
					},
				},
			},
		},
	}
}

func integrationDatabaseURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("INGESTION_TEST_DATABASE_URL"); url != "" {
		return url
	}
	t.Skip("set INGESTION_TEST_DATABASE_URL to run PostgreSQL integration tests")
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
		WHERE nspname = 'core_ingestion' OR nspname LIKE 'tenant_%'
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

func createTenantTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) {
	t.Helper()

	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pgx.Identifier{schemaName}.Sanitize())); err != nil {
		t.Fatalf("create tenant schema: %v", err)
	}
	query := fmt.Sprintf(`
		CREATE TABLE %s (
			id UUID NOT NULL PRIMARY KEY,
			object_id TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			valid_until TIMESTAMPTZ NOT NULL DEFAULT 'INFINITY',
			amount DOUBLE PRECISION NOT NULL,
			status TEXT NOT NULL
		)
	`, pgx.Identifier{schemaName, tableName}.Sanitize())
	if _, err := pool.Exec(ctx, query); err != nil {
		t.Fatalf("create tenant table: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE UNIQUE INDEX ON %s (object_id)", pgx.Identifier{schemaName, tableName}.Sanitize())); err != nil {
		t.Fatalf("create tenant object_id unique index: %v", err)
	}
}

func tenantSchemaName(tenantID uuid.UUID) string {
	return "tenant_" + strings.ReplaceAll(tenantID.String(), "-", "")
}

func assertTenantRowValues(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schemaName, tableName, objectID, expectedStatus string, expectedAmount float64) {
	t.Helper()
	var status string
	var amount float64
	query := fmt.Sprintf(`SELECT status, amount FROM %s WHERE object_id = $1`, pgx.Identifier{schemaName, tableName}.Sanitize())
	if err := pool.QueryRow(ctx, query, objectID).Scan(&status, &amount); err != nil {
		t.Fatalf("load tenant row for %s: %v", objectID, err)
	}
	if status != expectedStatus {
		t.Fatalf("expected status %s for %s, got %s", expectedStatus, objectID, status)
	}
	if amount != expectedAmount {
		t.Fatalf("expected amount %.2f for %s, got %.2f", expectedAmount, objectID, amount)
	}
}

func assertCoreIngestionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string, expected int) {
	t.Helper()
	var actual int
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tableName)
	if err := pool.QueryRow(ctx, query).Scan(&actual); err != nil {
		t.Fatalf("count rows in %s: %v", tableName, err)
	}
	if actual != expected {
		t.Fatalf("expected %d rows in %s, got %d", expected, tableName, actual)
	}
}

func integrationUUIDSequence(count int) []uuid.UUID {
	values := make([]uuid.UUID, count)
	for i := 0; i < count; i++ {
		values[i] = uuid.MustParse(fmt.Sprintf("90000000-0000-0000-0000-%012d", i+1))
	}
	return values
}
