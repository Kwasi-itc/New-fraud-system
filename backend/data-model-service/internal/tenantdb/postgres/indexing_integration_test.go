package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

func openDisposableIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DATA_MODEL_INTEGRATION_DATABASE_URL"))
	if dsn == "" {
		t.Skip("DATA_MODEL_INTEGRATION_DATABASE_URL not set; skipping Postgres CONCURRENTLY integration test")
	}
	lower := strings.ToLower(dsn)
	if !strings.Contains(lower, "disposable") && !strings.Contains(lower, "bench") && !strings.Contains(lower, "test") {
		t.Fatalf("refusing to run integration test against non-disposable database URL: %s", dsn)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCreateManagedIndexConcurrentlyDoesNotBlockWritesAndRebuildsInvalidIndex(t *testing.T) {
	pool := openDisposableIntegrationPool(t)
	ctx := context.Background()
	mgr := NewSchemaManager(pool)

	tenantID := uuid.New()
	schemaName := "tenant_" + strings.ReplaceAll(tenantID.String(), "-", "")
	rec := tenant.Tenant{
		ID:         tenantID,
		Name:       "Concurrent Index Test",
		SchemaName: schemaName,
		Status:     tenant.StatusActive,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	table := datamodel.Table{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "transactions",
	}

	if err := mgr.ProvisionTenantSchema(ctx, rec); err != nil {
		t.Fatalf("ProvisionTenantSchema() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", sanitizeIdentifier(schemaName)))
	})
	if err := mgr.CreateTable(ctx, rec, table); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	for _, field := range []datamodel.Field{
		{Name: "merchant_id", DataType: datamodel.DataTypeString},
		{Name: "created_at", DataType: datamodel.DataTypeTimestamp},
		{Name: "amount", DataType: datamodel.DataTypeFloat},
	} {
		if err := mgr.AddField(ctx, rec, table, field); err != nil {
			t.Fatalf("AddField(%s) error = %v", field.Name, err)
		}
	}

	// Seed 25k rows so CREATE INDEX CONCURRENTLY has real build duration while writers run.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, object_id, updated_at, merchant_id, created_at, amount)
		SELECT gen_random_uuid(), 'obj_' || g, NOW(), 'm_' || (g %% 100), NOW() - ((g %% 1000) || ' minutes')::interval, (g %% 500)::double precision
		FROM generate_series(1, 25000) AS g
	`, sanitizeIdentifier(schemaName, table.Name))); err != nil {
		t.Fatalf("seed rows error = %v", err)
	}

	// 1. Concurrent writes keep succeeding during CREATE INDEX CONCURRENTLY.
	job := datamodel.IndexJob{
		ID:             uuid.New(),
		TenantID:       tenantID,
		TableID:        &table.ID,
		TableName:      table.Name,
		IndexType:      datamodel.IndexJobTypeSearch,
		Columns:        []string{"merchant_id", "created_at"},
		IncludeColumns: []string{"amount"},
	}

	var writeErr atomic.Value
	var writesDone atomic.Int64
	stopWrites := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stopWrites:
					return
				default:
					i++
					objID := fmt.Sprintf("live_w%d_%d", workerID, i)
					_, err := pool.Exec(ctx, fmt.Sprintf(`
						INSERT INTO %s (id, object_id, updated_at, merchant_id, created_at, amount)
						VALUES (gen_random_uuid(), $1, NOW(), 'm_live', NOW(), 42.5)
					`, sanitizeIdentifier(schemaName, table.Name)), objID)
					if err != nil {
						writeErr.Store(err)
						return
					}
					writesDone.Add(1)
				}
			}
		}(w)
	}

	if err := mgr.CreateManagedIndex(ctx, rec, table, job); err != nil {
		close(stopWrites)
		wg.Wait()
		t.Fatalf("CreateManagedIndex() error = %v", err)
	}
	close(stopWrites)
	wg.Wait()
	if v := writeErr.Load(); v != nil {
		t.Fatalf("concurrent insert failed during CONCURRENTLY build: %v", v)
	}
	if writesDone.Load() == 0 {
		t.Fatal("expected concurrent inserts to execute while index built")
	}

	state, err := mgr.GetManagedIndexState(ctx, rec, table, job)
	if err != nil {
		t.Fatalf("GetManagedIndexState() error = %v", err)
	}
	if !state.Exists || !state.Valid {
		t.Fatalf("expected Exists=true, Valid=true, got %+v", state)
	}

	// 2. Pre-existing INVALID index with exact target name is detected, dropped, and rebuilt validly.
	if err := mgr.DropManagedIndex(ctx, rec, state.Name); err != nil {
		t.Fatalf("DropManagedIndex() error = %v", err)
	}
	// Insert duplicate merchant_id rows and fail CREATE UNIQUE INDEX CONCURRENTLY with the target name -> leaves indisvalid = false.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, object_id, updated_at, merchant_id, created_at, amount)
		VALUES (gen_random_uuid(), 'dup_a', NOW(), 'dup_merchant', '2026-11-25T00:00:00Z', 10),
		       (gen_random_uuid(), 'dup_b', NOW(), 'dup_merchant', '2026-11-25T00:00:00Z', 10)
	`, sanitizeIdentifier(schemaName, table.Name))); err != nil {
		t.Fatalf("insert duplicate rows error = %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE UNIQUE INDEX CONCURRENTLY %s ON %s ("merchant_id", "created_at")`,
		sanitizeIdentifier(state.Name),
		sanitizeIdentifier(schemaName, table.Name),
	)); err == nil {
		t.Fatal("expected CREATE UNIQUE INDEX CONCURRENTLY to fail on duplicate values and leave an INVALID index")
	}

	invalidState, err := mgr.GetManagedIndexState(ctx, rec, table, job)
	if err != nil {
		t.Fatalf("GetManagedIndexState(invalid) error = %v", err)
	}
	if !invalidState.Exists || invalidState.Valid {
		t.Fatalf("expected Exists=true, Valid=false for leftover invalid index, got %+v", invalidState)
	}

	if err := mgr.CreateManagedIndex(ctx, rec, table, job); err != nil {
		t.Fatalf("CreateManagedIndex(rebuild invalid) error = %v", err)
	}
	rebuiltState, err := mgr.GetManagedIndexState(ctx, rec, table, job)
	if err != nil {
		t.Fatalf("GetManagedIndexState(rebuilt) error = %v", err)
	}
	if !rebuiltState.Exists || !rebuiltState.Valid {
		t.Fatalf("expected rebuilt index Exists=true, Valid=true, got %+v", rebuiltState)
	}
}
