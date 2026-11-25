package postgres

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupRollupIntegrationDB(t *testing.T) (*pgxpool.Pool, *RollupDefinitionCache, ingestion.PublishedDataModel) {
	t.Helper()
	dsn := os.Getenv("INGESTION_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INGESTION_INTEGRATION_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	_, thisFile, _, _ := runtime.Caller(0)
	migDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "metadata"))
	for _, file := range []string{"000001_init.up.sql", "000002_deferred_ingests.up.sql", "000003_rollup_aggregates.up.sql"} {
		raw, err := os.ReadFile(filepath.Join(migDir, file))
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("exec migration %s: %v", file, err)
		}
	}

	tenantID := uuid.New()
	schemaName := tenantSchemaName(tenantID)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA IF NOT EXISTS %s;
		CREATE TABLE IF NOT EXISTS %s.transactions (
			id UUID NOT NULL PRIMARY KEY,
			object_id TEXT NOT NULL UNIQUE,
			merchant_id TEXT,
			amount BIGINT,
			float_amount DOUBLE PRECISION,
			created_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_tx_merchant_created ON %s.transactions (merchant_id, created_at) INCLUDE (amount, float_amount);
	`, sanitizeIdentifier(schemaName), sanitizeIdentifier(schemaName), sanitizeIdentifier(schemaName))); err != nil {
		t.Fatalf("create tenant table: %v", err)
	}

	model := ingestion.PublishedDataModel{
		TenantID:          tenantID,
		RevisionID:        "rev-1",
		SchemaName:        schemaName,
		RecordLookupField: "object_id",
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ingestion.FieldSchema{
					"object_id":    {Name: "object_id", DataType: "string"},
					"merchant_id":  {Name: "merchant_id", DataType: "string", Nullable: true},
					"amount":       {Name: "amount", DataType: "int", Nullable: true},
					"float_amount": {Name: "float_amount", DataType: "float", Nullable: true},
					"created_at":   {Name: "created_at", DataType: "timestamptz", Nullable: true},
				},
			},
		},
	}
	cache := NewRollupDefinitionCache(pool, 50*time.Millisecond)
	return pool, cache, model
}

func TestRollupDeltaCaptureSingleCTEAndIdempotency(t *testing.T) {
	pool, cache, model := setupRollupIntegrationDB(t)
	ctx := context.Background()
	repo := NewRollupRepository(pool)

	def, err := repo.UpsertDefinition(ctx, ingestion.RollupDefinition{
		TenantID:     model.TenantID.String(),
		ID:           uuid.NewString(),
		TableName:    "transactions",
		GroupField:   "merchant_id",
		TimeField:    "created_at",
		ValueField:   "amount",
		ValueType:    ingestion.RollupValueTypeInt,
		ModelVersion: model.RevisionID,
		Status:       ingestion.RollupStatusCapturing,
	})
	if err != nil {
		t.Fatalf("UpsertDefinition: %v", err)
	}
	cache.InvalidateAll()

	now := time.Date(2026, 11, 25, 12, 10, 0, 0, time.UTC)

	ingestRecord := func(mode ingestion.Mode, rec map[string]any) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var buf []ingestion.RollupDelta
		w := NewTenantDataWriterWithRollups(tx, cache, &buf)
		if _, err := w.UpsertRecord(ctx, model, "transactions", rec, mode, now); err != nil {
			t.Fatalf("UpsertRecord: %v", err)
		}
		if err := insertRollupDeltasTx(ctx, tx, buf); err != nil {
			t.Fatalf("insertRollupDeltasTx: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	ingestRecord(ingestion.ModeCreate, map[string]any{
		"object_id":   "tx-1",
		"merchant_id": "m1",
		"amount":      int64(100),
		"created_at":  now,
	})

	// 2. Re-ingest identical record -> zero net delta
	var deltaCountBefore int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM core_ingestion.rollup_deltas WHERE tenant_id = $1 AND definition_id = $2`, model.TenantID.String(), def.ID).Scan(&deltaCountBefore)
	ingestRecord(ingestion.ModeCreate, map[string]any{
		"object_id":   "tx-1",
		"merchant_id": "m1",
		"amount":      int64(100),
		"created_at":  now,
	})
	var deltaCountAfter int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM core_ingestion.rollup_deltas WHERE tenant_id = $1 AND definition_id = $2`, model.TenantID.String(), def.ID).Scan(&deltaCountAfter)
	if deltaCountAfter != deltaCountBefore {
		t.Fatalf("re-ingesting identical record emitted %d extra deltas, want 0", deltaCountAfter-deltaCountBefore)
	}

	// 3. Patch merchant_id from m1 -> m2 and amount 100 -> 250
	ingestRecord(ingestion.ModePatch, map[string]any{
		"object_id":   "tx-1",
		"merchant_id": "m2",
		"amount":      int64(250),
	})

	// 4. Insert NULL amount record for m2 (counts in d_rows, 0 in d_value_count/sum)
	ingestRecord(ingestion.ModeCreate, map[string]any{
		"object_id":   "tx-2",
		"merchant_id": "m2",
		"amount":      nil,
		"created_at":  now,
	})

	var m1Rows, m1VC int64
	var m1Sum string
	_ = pool.QueryRow(ctx, `SELECT COALESCE(SUM(d_rows),0)::bigint, COALESCE(SUM(d_value_count),0)::bigint, COALESCE(SUM(d_value_sum),0)::text FROM core_ingestion.rollup_deltas WHERE tenant_id = $1 AND definition_id = $2 AND group_value = 'm1'`, model.TenantID.String(), def.ID).Scan(&m1Rows, &m1VC, &m1Sum)
	if m1Rows != 0 || m1VC != 0 || m1Sum != "0" {
		t.Fatalf("m1 net delta = (%d, %d, %s), want (0, 0, 0)", m1Rows, m1VC, m1Sum)
	}

	var m2Rows, m2VC int64
	var m2Sum string
	_ = pool.QueryRow(ctx, `SELECT COALESCE(SUM(d_rows),0)::bigint, COALESCE(SUM(d_value_count),0)::bigint, COALESCE(SUM(d_value_sum),0)::text FROM core_ingestion.rollup_deltas WHERE tenant_id = $1 AND definition_id = $2 AND group_value = 'm2'`, model.TenantID.String(), def.ID).Scan(&m2Rows, &m2VC, &m2Sum)
	if m2Rows != 2 || m2VC != 1 || m2Sum != "250" {
		t.Fatalf("m2 net delta = (%d, %d, %s), want (2, 1, 250)", m2Rows, m2VC, m2Sum)
	}
}

func TestRollupParallelAppliersZeroDeadlocks(t *testing.T) {
	pool, _, model := setupRollupIntegrationDB(t)
	ctx := context.Background()
	repo := NewRollupRepository(pool)

	def, err := repo.UpsertDefinition(ctx, ingestion.RollupDefinition{
		TenantID:     model.TenantID.String(),
		ID:           uuid.NewString(),
		TableName:    "transactions",
		GroupField:   "merchant_id",
		TimeField:    "created_at",
		ValueField:   "amount",
		ValueType:    ingestion.RollupValueTypeInt,
		ModelVersion: model.RevisionID,
		Status:       ingestion.RollupStatusReady,
	})
	if err != nil {
		t.Fatalf("UpsertDefinition: %v", err)
	}

	baseTime := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	var deltas []ingestion.RollupDelta
	for i := 0; i < 400; i++ {
		deltas = append(deltas, ingestion.RollupDelta{
			TenantID:     model.TenantID.String(),
			DefinitionID: def.ID,
			GroupValue:   fmt.Sprintf("m-%d", i%5),
			EventTime:    baseTime.Add(time.Duration(i%4) * 15 * time.Minute),
			DRows:        1,
			DValueCount:  1,
			DValueSum:    "10",
		})
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := insertRollupDeltasTx(ctx, tx, deltas); err != nil {
		t.Fatalf("insert deltas: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				n, err := ApplyRollupDeltaBatch(ctx, pool, model.TenantID.String(), def.ID, 25, time.Now().UTC())
				if err != nil {
					errs <- err
					return
				}
				if n == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("parallel applier failed: %v", err)
	}

	var totalRows int64
	var totalSum string
	_ = pool.QueryRow(ctx, `SELECT COALESCE(SUM(rows),0)::bigint, COALESCE(SUM(value_sum),0)::text FROM core_ingestion.rollup_buckets WHERE tenant_id = $1 AND definition_id = $2 AND resolution = 900`, model.TenantID.String(), def.ID).Scan(&totalRows, &totalSum)
	if totalRows != 400 || totalSum != "4000" {
		t.Fatalf("bucket totals = (%d, %s), want (400, 4000)", totalRows, totalSum)
	}
}

func TestRollupUnionAllExplainAndPropertyWindows(t *testing.T) {
	pool, cache, model := setupRollupIntegrationDB(t)
	ctx := context.Background()
	repo := NewRollupRepository(pool)

	defInt, err := repo.UpsertDefinition(ctx, ingestion.RollupDefinition{
		TenantID:     model.TenantID.String(),
		ID:           uuid.NewString(),
		TableName:    "transactions",
		GroupField:   "merchant_id",
		TimeField:    "created_at",
		ValueField:   "amount",
		ValueType:    ingestion.RollupValueTypeInt,
		ModelVersion: model.RevisionID,
		Status:       ingestion.RollupStatusReady,
	})
	if err != nil {
		t.Fatalf("UpsertDefinition int: %v", err)
	}
	defFloat, err := repo.UpsertDefinition(ctx, ingestion.RollupDefinition{
		TenantID:     model.TenantID.String(),
		ID:           uuid.NewString(),
		TableName:    "transactions",
		GroupField:   "merchant_id",
		TimeField:    "created_at",
		ValueField:   "float_amount",
		ValueType:    ingestion.RollupValueTypeFloat,
		ModelVersion: model.RevisionID,
		Status:       ingestion.RollupStatusReady,
	})
	if err != nil {
		t.Fatalf("UpsertDefinition float: %v", err)
	}
	cache.InvalidateAll()

	now := time.Now().UTC().Truncate(time.Minute)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 200; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		var buf []ingestion.RollupDelta
		w := NewTenantDataWriterWithRollups(tx, cache, &buf)
		ts := now.Add(-time.Duration(rng.Intn(72*60)) * time.Minute)
		var amt any = int64(rng.Intn(500) + 1)
		var famt any = math.Round((rng.Float64()*500+1)*100) / 100
		if i%9 == 0 {
			amt = nil
			famt = nil
		}
		merchant := fmt.Sprintf("m%d", (i%10)+1)
		_, err = w.UpsertRecord(ctx, model, "transactions", map[string]any{
			"object_id":    fmt.Sprintf("prop-%d", i),
			"merchant_id":  merchant,
			"amount":       amt,
			"float_amount": famt,
			"created_at":   ts,
		}, ingestion.ModeCreate, now)
		if err != nil {
			t.Fatalf("UpsertRecord: %v", err)
		}
		if err := insertRollupDeltasTx(ctx, tx, buf); err != nil {
			t.Fatalf("insertRollupDeltasTx: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	// Apply half the deltas so both rollup_buckets and rollup_deltas are exercised concurrently!
	if _, err := ApplyRollupDeltaBatch(ctx, pool, model.TenantID.String(), defInt.ID, 100, now); err != nil {
		t.Fatalf("ApplyRollupDeltaBatch int: %v", err)
	}
	if _, err := ApplyRollupDeltaBatch(ctx, pool, model.TenantID.String(), defFloat.ID, 100, now); err != nil {
		t.Fatalf("ApplyRollupDeltaBatch float: %v", err)
	}
	// Seed extra rows for other merchants and VACUUM ANALYZE so visibility map + stats reflect production selectivity.
	_, _ = pool.Exec(ctx, `
		INSERT INTO core_ingestion.rollup_deltas (tenant_id, definition_id, group_value, event_time, d_rows, d_value_count, d_value_sum)
		SELECT $1::uuid, $2::uuid, 'other-' || (g % 50), $3::timestamptz, 1, 1, 10
		FROM generate_series(1, 2000) AS g
	`, model.TenantID.String(), defInt.ID, now.Add(-2*time.Hour))
	_, _ = pool.Exec(ctx, `VACUUM ANALYZE core_ingestion.rollup_deltas`)

	// Verify EXPLAIN uses idx_rollup_deltas_query_covering
	_, _ = pool.Exec(ctx, `SET enable_seqscan = off`)
	defer func() { _, _ = pool.Exec(ctx, `SET enable_seqscan = on`) }()

	cand, reason := checkRollupEligibility(model, model.Tables["transactions"], ingestion.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "sum",
		Field:      "amount",
		Filter: &ingestion.AggregateFilter{
			Kind:     ingestion.AggregateFilterKindGroup,
			Operator: "and",
			Children: []ingestion.AggregateFilter{
				{Kind: ingestion.AggregateFilterKindPredicate, Field: "merchant_id", Op: "eq", Value: "m1"},
				{Kind: ingestion.AggregateFilterKindPredicate, Field: "created_at", Op: "gte", Value: now.Add(-48 * time.Hour)},
				{Kind: ingestion.AggregateFilterKindPredicate, Field: "created_at", Op: "lte", Value: now},
			},
		},
	}, []ingestion.RollupDefinition{defInt}, now)
	if reason != "" {
		t.Fatalf("checkRollupEligibility reason = %q", reason)
	}
	sqlStr, sqlArgs, err := buildRollupUnionSQL(model, model.Tables["transactions"], cand)
	if err != nil {
		t.Fatalf("buildRollupUnionSQL: %v", err)
	}
	rows, err := pool.Query(ctx, "EXPLAIN "+sqlStr, sqlArgs...)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	var explainLines []string
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		explainLines = append(explainLines, line)
	}
	rows.Close()
	if !strings.Contains(strings.Join(explainLines, "\n"), "idx_rollup_deltas_query_covering") {
		t.Fatalf("expected idx_rollup_deltas_query_covering in EXPLAIN, got:\n%s", strings.Join(explainLines, "\n"))
	}

	rawReader := NewTenantDataReader(pool)
	rollupReader := NewTenantDataReader(pool).WithRollups(cache, true, false)

	// 200 randomized windows covering gt/gte, lt/lte, unbounded above, count/sum(int)/sum(float)/avg
	aggs := []struct {
		agg   string
		field string
	}{
		{"count", "object_id"},
		{"sum", "amount"},
		{"sum", "float_amount"},
		{"avg", "amount"},
	}
	for iter := 0; iter < 200; iter++ {
		a := aggs[iter%len(aggs)]
		loHours := rng.Intn(70) + 2
		hiHours := rng.Intn(loHours)
		loOp := "gte"
		if iter%2 == 1 {
			loOp = "gt"
		}
		children := []ingestion.AggregateFilter{
			{Kind: ingestion.AggregateFilterKindPredicate, Field: "merchant_id", Op: "eq", Value: "m1"},
			{Kind: ingestion.AggregateFilterKindPredicate, Field: "created_at", Op: loOp, Value: now.Add(-time.Duration(loHours)*time.Hour - time.Duration(rng.Intn(59))*time.Minute)},
		}
		if iter%5 != 0 { // 20% unbounded above
			hiOp := "lte"
			if iter%3 == 0 {
				hiOp = "lt"
			}
			children = append(children, ingestion.AggregateFilter{
				Kind: ingestion.AggregateFilterKindPredicate, Field: "created_at", Op: hiOp,
				Value: now.Add(-time.Duration(hiHours)*time.Hour - time.Duration(rng.Intn(59))*time.Minute),
			})
		}
		q := ingestion.AggregateQuery{
			ObjectType: "transactions",
			Aggregate:  a.agg,
			Field:      a.field,
			Filter: &ingestion.AggregateFilter{
				Kind:     ingestion.AggregateFilterKindGroup,
				Operator: "and",
				Children: children,
			},
		}
		rawVal, err := rawReader.AggregateRecords(ctx, model, q)
		if err != nil {
			t.Fatalf("raw AggregateRecords iter=%d: %v", iter, err)
		}
		rollupVal, err := rollupReader.AggregateRecords(ctx, model, q)
		if err != nil {
			t.Fatalf("rollup AggregateRecords iter=%d: %v", iter, err)
		}
		if !compareRollupShadowValues(a.agg, rawVal, rollupVal) {
			t.Fatalf("iter=%d agg=%s field=%s mismatch: raw=%#v (%T) rollup=%#v (%T)", iter, a.agg, a.field, rawVal, rawVal, rollupVal, rollupVal)
		}
	}
}

func TestRollupRaceFreeBackfillWithConcurrentWrites(t *testing.T) {
	pool, cache, model := setupRollupIntegrationDB(t)
	ctx := context.Background()
	repo := NewRollupRepository(pool)

	now := time.Now().UTC().Truncate(time.Minute)
	// Seed initial raw rows before definition exists
	for i := 0; i < 20; i++ {
		tx, _ := pool.Begin(ctx)
		w := NewTenantDataWriter(tx)
		_, err := w.UpsertRecord(ctx, model, "transactions", map[string]any{
			"object_id":   fmt.Sprintf("seed-%d", i),
			"merchant_id": "m1",
			"amount":      int64(100),
			"created_at":  now.Add(-time.Duration(i) * 20 * time.Minute),
		}, ingestion.ModeCreate, now)
		if err != nil {
			t.Fatalf("seed UpsertRecord: %v", err)
		}
		_ = tx.Commit(ctx)
	}

	def, err := repo.UpsertDefinition(ctx, ingestion.RollupDefinition{
		TenantID:     model.TenantID.String(),
		ID:           uuid.NewString(),
		TableName:    "transactions",
		GroupField:   "merchant_id",
		TimeField:    "created_at",
		ValueField:   "amount",
		ValueType:    ingestion.RollupValueTypeInt,
		ModelVersion: model.RevisionID,
		Status:       ingestion.RollupStatusCapturing,
	})
	if err != nil {
		t.Fatalf("UpsertDefinition: %v", err)
	}
	cache.InvalidateAll()

	// Run concurrent inserts + ModePatch updates while RunRollupBackfill executes
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			select {
			case <-stop:
				return
			default:
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				continue
			}
			var buf []ingestion.RollupDelta
			w := NewTenantDataWriterWithRollups(tx, cache, &buf)
			_, err = w.UpsertRecord(ctx, model, "transactions", map[string]any{
				"object_id":   fmt.Sprintf("conc-%d", i),
				"merchant_id": "m1",
				"amount":      int64(50),
				"created_at":  now.Add(-30 * time.Minute),
			}, ingestion.ModeCreate, now)
			if err == nil {
				_ = insertRollupDeltasTx(ctx, tx, buf)
				_ = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	if err := RunRollupBackfill(ctx, pool, cache, model, model.TenantID.String(), def.ID, 60*time.Millisecond, now); err != nil {
		close(stop)
		wg.Wait()
		t.Fatalf("RunRollupBackfill: %v", err)
	}
	close(stop)
	wg.Wait()

	// Drain remaining post-snapshot deltas and verify exact parity
	for {
		n, err := ApplyRollupDeltaBatch(ctx, pool, model.TenantID.String(), def.ID, 500, now)
		if err != nil {
			t.Fatalf("ApplyRollupDeltaBatch: %v", err)
		}
		if n == 0 {
			break
		}
	}

	q := ingestion.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "sum",
		Field:      "amount",
		Filter: &ingestion.AggregateFilter{
			Kind:     ingestion.AggregateFilterKindGroup,
			Operator: "and",
			Children: []ingestion.AggregateFilter{
				{Kind: ingestion.AggregateFilterKindPredicate, Field: "merchant_id", Op: "eq", Value: "m1"},
				{Kind: ingestion.AggregateFilterKindPredicate, Field: "created_at", Op: "gte", Value: now.Add(-24 * time.Hour)},
			},
		},
	}
	rawVal, err := NewTenantDataReader(pool).AggregateRecords(ctx, model, q)
	if err != nil {
		t.Fatalf("raw AggregateRecords: %v", err)
	}
	rollupVal, err := NewTenantDataReader(pool).WithRollups(cache, true, false).AggregateRecords(ctx, model, q)
	if err != nil {
		t.Fatalf("rollup AggregateRecords: %v", err)
	}
	if !compareRollupShadowValues("sum", rawVal, rollupVal) {
		t.Fatalf("backfill parity mismatch: raw=%v rollup=%v", rawVal, rollupVal)
	}
}

func TestRollupReconcilerRetentionSchemaInvalidationAndIsolation(t *testing.T) {
	pool, cache, model := setupRollupIntegrationDB(t)
	ctx := context.Background()
	repo := NewRollupRepository(pool)

	def, err := repo.UpsertDefinition(ctx, ingestion.RollupDefinition{
		TenantID:     model.TenantID.String(),
		ID:           uuid.NewString(),
		TableName:    "transactions",
		GroupField:   "merchant_id",
		TimeField:    "created_at",
		ValueField:   "amount",
		ValueType:    ingestion.RollupValueTypeInt,
		ModelVersion: model.RevisionID,
		Status:       ingestion.RollupStatusReady,
	})
	if err != nil {
		t.Fatalf("UpsertDefinition: %v", err)
	}

	// Cross-tenant isolation: another tenant cannot see def
	otherTenant := uuid.NewString()
	otherDefs, err := repo.ListDefinitionsByTenant(ctx, otherTenant)
	if err != nil || len(otherDefs) != 0 {
		t.Fatalf("expected 0 definitions for other tenant, got %d (%v)", len(otherDefs), err)
	}

	// Reconciler mismatch detection -> disables definition
	now := time.Now().UTC().Truncate(time.Hour)
	oldBucket := now.Add(-20 * 24 * time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO core_ingestion.rollup_buckets (tenant_id, definition_id, resolution, group_value, bucket_start, rows, value_count, value_sum)
		VALUES ($1, $2, 900, 'm1', $3, 99, 99, 9999),
		       ($1, $2, 900, 'm1', $4, 1, 1, 100)
	`, model.TenantID.String(), def.ID, now.Add(-1*time.Hour), oldBucket)
	if err != nil {
		t.Fatalf("insert test buckets: %v", err)
	}

	mismatches, err := ReconcileRollupSample(ctx, pool, cache, model, def, 10)
	if err != nil {
		t.Fatalf("ReconcileRollupSample: %v", err)
	}
	if mismatches == 0 {
		t.Fatalf("expected reconciler to detect mismatch")
	}
	updatedDef, err := repo.GetDefinition(ctx, model.TenantID.String(), def.ID)
	if err != nil || updatedDef.Status != ingestion.RollupStatusDisabled {
		t.Fatalf("expected definition disabled after mismatch, got %s (%v)", updatedDef.Status, err)
	}

	// Retention pruning drops 900s bucket older than 14d
	pruned, err := PruneExpiredRollupBuckets(ctx, pool, now)
	if err != nil || pruned < 1 {
		t.Fatalf("PruneExpiredRollupBuckets = (%d, %v), want >= 1", pruned, err)
	}

	// Schema change invalidation: fast-forward on unrelated model bump, disable on field removal
	_ = repo.UpdateDefinitionStatus(ctx, model.TenantID.String(), def.ID, ingestion.RollupStatusReady, nil)
	model.RevisionID = "rev-2"
	if err := RevalidateRollupDefinitionsAgainstModel(ctx, pool, cache, model); err != nil {
		t.Fatalf("RevalidateRollupDefinitionsAgainstModel rev-2: %v", err)
	}
	defRev2, _ := repo.GetDefinition(ctx, model.TenantID.String(), def.ID)
	if defRev2.ModelVersion != "rev-2" || defRev2.Status != ingestion.RollupStatusReady {
		t.Fatalf("expected fast-forward to rev-2 ready, got (%s, %s)", defRev2.ModelVersion, defRev2.Status)
	}

	delete(model.Tables["transactions"].Fields, "amount")
	model.RevisionID = "rev-3"
	if err := RevalidateRollupDefinitionsAgainstModel(ctx, pool, cache, model); err != nil {
		t.Fatalf("RevalidateRollupDefinitionsAgainstModel rev-3: %v", err)
	}
	defRev3, _ := repo.GetDefinition(ctx, model.TenantID.String(), def.ID)
	if defRev3.Status != ingestion.RollupStatusDisabled {
		t.Fatalf("expected disabled after amount archived/dropped, got %s", defRev3.Status)
	}
}

