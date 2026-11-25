package postgres

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultRollupApplierBatchSize = 5000

type rollupBucketKey struct {
	resolution  int32
	groupValue  string
	bucketStart time.Time
}

type rollupBucketAccum struct {
	rows       int64
	valueCount int64
	valueSum   *big.Rat
}

// ApplyRollupDeltaBatch drains up to batchSize pending deltas for a 'ready' rollup definition,
// discards pre-backfill-snapshot deltas, folds across {900, 3600, 86400} UTC buckets,
// and upserts them in deterministic (resolution, group_value, bucket_start) order.
// Returns (drainedCount, oldestRemainingCreatedOrZero, error).
func ApplyRollupDeltaBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID, definitionID string,
	batchSize int,
	now time.Time,
) (int, error) {
	if batchSize <= 0 {
		batchSize = DefaultRollupApplierBatchSize
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var status string
	var backfillSnapshot *string
	err = tx.QueryRow(ctx, `
		SELECT status, backfill_snapshot::text
		FROM core_ingestion.rollup_definitions
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, definitionID).Scan(&status, &backfillSnapshot)
	if err != nil {
		return 0, err
	}
	if status != string(ingestion.RollupStatusReady) {
		return 0, nil
	}

	const drainStmt = `
		DELETE FROM core_ingestion.rollup_deltas
		WHERE tenant_id = $1 AND definition_id = $2
			AND id IN (
				SELECT id
				FROM core_ingestion.rollup_deltas
				WHERE tenant_id = $1 AND definition_id = $2
				ORDER BY id
				LIMIT $3
				FOR UPDATE SKIP LOCKED
			)
		RETURNING group_value, event_time, d_rows, d_value_count, d_value_sum::text,
			($4::pg_snapshot IS NOT NULL AND pg_visible_in_snapshot(xid, $4::pg_snapshot)) AS pre_snapshot
	`
	rows, err := tx.Query(ctx, drainStmt, tenantID, definitionID, batchSize, backfillSnapshot)
	if err != nil {
		return 0, err
	}

	type drainedRow struct {
		groupValue  string
		eventTime   time.Time
		dRows       int64
		dValueCount int64
		dValueSum   string
		preSnapshot bool
	}
	var drained []drainedRow
	for rows.Next() {
		var r drainedRow
		if err := rows.Scan(&r.groupValue, &r.eventTime, &r.dRows, &r.dValueCount, &r.dValueSum, &r.preSnapshot); err != nil {
			rows.Close()
			return 0, err
		}
		drained = append(drained, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(drained) == 0 {
		return 0, tx.Commit(ctx)
	}

	folded := make(map[rollupBucketKey]*rollupBucketAccum)
	for _, r := range drained {
		if r.preSnapshot {
			continue
		}
		deltaSum := new(big.Rat)
		if _, ok := deltaSum.SetString(strings.TrimSpace(r.dValueSum)); !ok {
			return 0, fmt.Errorf("invalid delta numeric sum %q", r.dValueSum)
		}
		for _, res := range ingestion.RollupResolutions {
			retention := ingestion.RollupRetentionForResolution(res)
			if retention > 0 && r.eventTime.UTC().Before(now.UTC().Add(-retention)) {
				continue
			}
			bucketStart := ingestion.AlignRollupBucketUTC(r.eventTime, res)
			key := rollupBucketKey{
				resolution:  res,
				groupValue:  r.groupValue,
				bucketStart: bucketStart,
			}
			acc := folded[key]
			if acc == nil {
				acc = &rollupBucketAccum{valueSum: new(big.Rat)}
				folded[key] = acc
			}
			acc.rows += r.dRows
			acc.valueCount += r.dValueCount
			acc.valueSum.Add(acc.valueSum, deltaSum)
		}
	}

	if len(folded) > 0 {
		keys := make([]rollupBucketKey, 0, len(folded))
		for k := range folded {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].resolution != keys[j].resolution {
				return keys[i].resolution < keys[j].resolution
			}
			if keys[i].groupValue != keys[j].groupValue {
				return keys[i].groupValue < keys[j].groupValue
			}
			return keys[i].bucketStart.Before(keys[j].bucketStart)
		})

		if err := upsertRollupBucketsTx(ctx, tx, tenantID, definitionID, keys, folded); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(drained), nil
}

func upsertRollupBucketsTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, definitionID string,
	keys []rollupBucketKey,
	folded map[rollupBucketKey]*rollupBucketAccum,
) error {
	const chunkSize = 1000
	for start := 0; start < len(keys); start += chunkSize {
		end := start + chunkSize
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[start:end]
		var sb strings.Builder
		sb.WriteString(`INSERT INTO core_ingestion.rollup_buckets (tenant_id, definition_id, resolution, group_value, bucket_start, rows, value_count, value_sum) VALUES `)
		args := make([]any, 0, len(chunk)*8)
		for i, k := range chunk {
			if i > 0 {
				sb.WriteString(", ")
			}
			base := i*8 + 1
			fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::numeric)",
				base, base+1, base+2, base+3, base+4, base+5, base+6, base+7)
			acc := folded[k]
			args = append(args,
				tenantID,
				definitionID,
				k.resolution,
				k.groupValue,
				k.bucketStart.UTC(),
				acc.rows,
				acc.valueCount,
				formatBigRatNumeric(acc.valueSum),
			)
		}
		sb.WriteString(`
			ON CONFLICT (tenant_id, definition_id, resolution, group_value, bucket_start)
			DO UPDATE SET
				rows = core_ingestion.rollup_buckets.rows + EXCLUDED.rows,
				value_count = core_ingestion.rollup_buckets.value_count + EXCLUDED.value_count,
				value_sum = core_ingestion.rollup_buckets.value_sum + EXCLUDED.value_sum
		`)
		if _, err := tx.Exec(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func formatBigRatNumeric(r *big.Rat) string {
	if r == nil || r.Sign() == 0 {
		return "0"
	}
	if r.IsInt() {
		return r.Num().String()
	}
	return r.FloatString(18)
}
