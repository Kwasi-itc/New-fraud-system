package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunRollupBackfill executes the §6.6 race-free backfill state machine:
// 1. Reset definition to 'capturing' (applier stays paused while != 'ready') and clear backfill_snapshot.
// 2. Wait for definition cache TTL barrier across ingestion replicas.
// 3. Poll pg_snapshot_xmin(pg_current_snapshot()) >= xid_at_cache_expiry so all pre-capture writers finish.
// 4. Transition to 'backfilling', delete prior rollup_buckets for definition_id.
// 5. Open a REPEATABLE READ read-only tx, record S = pg_current_snapshot(), persist backfill_snapshot = S,
//    and stream grouped bucket aggregates into rollup_buckets in bounded batches.
// 6. Synchronously delete pre-S deltas (pg_visible_in_snapshot(xid, S)) and flip status = 'ready'.
func RunRollupBackfill(
	ctx context.Context,
	pool *pgxpool.Pool,
	cache *RollupDefinitionCache,
	model ingestion.PublishedDataModel,
	tenantID, definitionID string,
	cacheBarrierWait time.Duration,
	now time.Time,
) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	repo := NewRollupRepository(pool)
	def, err := repo.GetDefinition(ctx, tenantID, definitionID)
	if err != nil {
		return err
	}

	if err := repo.ResetDefinitionToCapturing(ctx, tenantID, definitionID); err != nil {
		return err
	}
	if cache != nil {
		cache.Invalidate(tenantID, def.TableName)
	}
	if cacheBarrierWait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cacheBarrierWait):
		}
	}

	// Barrier: wait until pg_snapshot_xmin(pg_current_snapshot()) >= xidAtExpiry.
	var xidAtExpiry uint64
	if err := pool.QueryRow(ctx, `SELECT pg_current_xact_id()::text::bigint`).Scan(&xidAtExpiry); err != nil {
		return err
	}
	for {
		var xmin uint64
		if err := pool.QueryRow(ctx, `SELECT pg_snapshot_xmin(pg_current_snapshot())::text::bigint`).Scan(&xmin); err != nil {
			return err
		}
		if xmin >= xidAtExpiry {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}

	// Step 3: Transition to 'backfilling' and clear prior buckets.
	if err := repo.UpdateDefinitionStatus(ctx, tenantID, definitionID, ingestion.RollupStatusBackfilling, nil); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM core_ingestion.rollup_buckets WHERE tenant_id = $1 AND definition_id = $2`, tenantID, definitionID); err != nil {
		return err
	}

	// Step 4: REPEATABLE READ read-only snapshot backfill.
	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = readTx.Rollback(context.Background())
	}()

	var snapshotText string
	if err := readTx.QueryRow(ctx, `SELECT pg_current_snapshot()::text`).Scan(&snapshotText); err != nil {
		return err
	}
	if err := repo.UpdateDefinitionStatus(ctx, tenantID, definitionID, ingestion.RollupStatusBackfilling, &snapshotText); err != nil {
		return err
	}

	schemaName := schemaNameForModel(model)
	qualifiedTable := sanitizeIdentifier(schemaName, def.TableName)
	groupCol := sanitizeIdentifier(def.GroupField)
	timeCol := sanitizeIdentifier(def.TimeField)
	valCol := sanitizeIdentifier(def.ValueField)

	valSumExpr := "0::numeric"
	if def.ValueType == ingestion.RollupValueTypeInt || def.ValueType == ingestion.RollupValueTypeFloat {
		valSumExpr = fmt.Sprintf("COALESCE(SUM(%s::numeric), 0)", valCol)
	}

	for _, res := range ingestion.RollupResolutions {
		retention := ingestion.RollupRetentionForResolution(res)
		cutoff := now.UTC().Add(-retention)
		query := fmt.Sprintf(`
			SELECT %s::text AS gv,
				date_bin(make_interval(secs => $1::int), %s, TIMESTAMPTZ '2000-01-01 00:00:00+00') AS bstart,
				COUNT(*)::bigint AS rows,
				COUNT(%s)::bigint AS vc,
				%s AS vs
			FROM %s
			WHERE %s IS NOT NULL AND %s IS NOT NULL AND %s >= $2
			GROUP BY 1, 2
			ORDER BY 1, 2
		`, groupCol, timeCol, valCol, valSumExpr, qualifiedTable, groupCol, timeCol, timeCol)

		rows, err := readTx.Query(ctx, query, int(res), cutoff)
		if err != nil {
			return err
		}

		type bucketBatchRow struct {
			gv     string
			bstart time.Time
			rows   int64
			vc     int64
			vs     string
		}
		var batch []bucketBatchRow
		flushBatch := func() error {
			if len(batch) == 0 {
				return nil
			}
			keys := make([]rollupBucketKey, len(batch))
			folded := make(map[rollupBucketKey]*rollupBucketAccum, len(batch))
			for i, b := range batch {
				k := rollupBucketKey{resolution: res, groupValue: b.gv, bucketStart: b.bstart.UTC()}
				keys[i] = k
				folded[k] = &rollupBucketAccum{rows: b.rows, valueCount: b.vc}
			}
			// Insert using raw numeric string via dedicated batch exec.
			writeTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				return err
			}
			defer func() { _ = writeTx.Rollback(context.Background()) }()
			if err := upsertRawBackfillBucketsTx(ctx, writeTx, tenantID, definitionID, res, batch); err != nil {
				return err
			}
			return writeTx.Commit(ctx)
		}

		for rows.Next() {
			var r bucketBatchRow
			if err := rows.Scan(&r.gv, &r.bstart, &r.rows, &r.vc, &r.vs); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, r)
			if len(batch) >= 1000 {
				if err := flushBatch(); err != nil {
					rows.Close()
					return err
				}
				batch = batch[:0]
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if err := flushBatch(); err != nil {
			return err
		}
		_ = keysPlaceholder()
	}

	if err := readTx.Commit(ctx); err != nil {
		return err
	}

	// Step 5: Synchronously delete pre-S deltas and flip status = 'ready'.
	finalTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = finalTx.Rollback(context.Background()) }()

	if _, err := finalTx.Exec(ctx, `
		DELETE FROM core_ingestion.rollup_deltas
		WHERE tenant_id = $1 AND definition_id = $2
			AND pg_visible_in_snapshot(xid, $3::pg_snapshot)
	`, tenantID, definitionID, snapshotText); err != nil {
		return err
	}
	if _, err := finalTx.Exec(ctx, `
		UPDATE core_ingestion.rollup_definitions
		SET status = 'ready', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, definitionID); err != nil {
		return err
	}
	if err := finalTx.Commit(ctx); err != nil {
		return err
	}
	if cache != nil {
		cache.Invalidate(tenantID, def.TableName)
	}
	return nil
}

func keysPlaceholder() struct{} { return struct{}{} }

func upsertRawBackfillBucketsTx[T interface {
	~struct {
		gv     string
		bstart time.Time
		rows   int64
		vc     int64
		vs     string
	}
}](ctx context.Context, tx pgx.Tx, tenantID, definitionID string, res int32, batch []T) error {
	if len(batch) == 0 {
		return nil
	}
	var args []any
	sql := "INSERT INTO core_ingestion.rollup_buckets (tenant_id, definition_id, resolution, group_value, bucket_start, rows, value_count, value_sum) VALUES "
	for i, item := range batch {
		if i > 0 {
			sql += ", "
		}
		s := struct {
			gv     string
			bstart time.Time
			rows   int64
			vc     int64
			vs     string
		}(item)
		base := i*8 + 1
		sql += fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::numeric)",
			base, base+1, base+2, base+3, base+4, base+5, base+6, base+7)
		args = append(args, tenantID, definitionID, res, s.gv, s.bstart.UTC(), s.rows, s.vc, s.vs)
	}
	sql += `
		ON CONFLICT (tenant_id, definition_id, resolution, group_value, bucket_start)
		DO UPDATE SET
			rows = core_ingestion.rollup_buckets.rows + EXCLUDED.rows,
			value_count = core_ingestion.rollup_buckets.value_count + EXCLUDED.value_count,
			value_sum = core_ingestion.rollup_buckets.value_sum + EXCLUDED.value_sum
	`
	_, err := tx.Exec(ctx, sql, args...)
	return err
}
