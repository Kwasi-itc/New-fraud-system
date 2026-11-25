package postgres

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReconcileRollupSample compares rollup_buckets + pending deltas against raw tenant table rows
// in a single REPEATABLE READ snapshot. On mismatch, increments rollup_reconcile_mismatch_total
// and transitions the definition to 'disabled' for safe fallback + clean rebuild.
func ReconcileRollupSample(
	ctx context.Context,
	pool *pgxpool.Pool,
	cache *RollupDefinitionCache,
	model ingestion.PublishedDataModel,
	def ingestion.RollupDefinition,
	sampleLimit int,
) (int, error) {
	if def.Status != ingestion.RollupStatusReady {
		return 0, nil
	}
	if sampleLimit <= 0 {
		sampleLimit = 50
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	rows, err := tx.Query(ctx, `
		SELECT resolution, group_value, bucket_start, rows, value_count, value_sum::text
		FROM core_ingestion.rollup_buckets
		WHERE tenant_id = $1 AND definition_id = $2
		ORDER BY bucket_start DESC, group_value
		LIMIT $3
	`, def.TenantID, def.ID, sampleLimit)
	if err != nil {
		return 0, err
	}

	type sampleBucket struct {
		res    int32
		gv     string
		bstart time.Time
		rows   int64
		vc     int64
		vs     string
	}
	var samples []sampleBucket
	for rows.Next() {
		var s sampleBucket
		if err := rows.Scan(&s.res, &s.gv, &s.bstart, &s.rows, &s.vc, &s.vs); err != nil {
			rows.Close()
			return 0, err
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	schemaName := schemaNameForModel(model)
	qualifiedTable := sanitizeIdentifier(schemaName, def.TableName)
	groupCol := sanitizeIdentifier(def.GroupField)
	timeCol := sanitizeIdentifier(def.TimeField)
	valCol := sanitizeIdentifier(def.ValueField)

	mismatches := 0
	for _, s := range samples {
		bend := s.bstart.UTC().Add(time.Duration(s.res) * time.Second)

		var dRows, dVC int64
		var dVS string
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(d_rows), 0)::bigint,
				COALESCE(SUM(d_value_count), 0)::bigint,
				COALESCE(SUM(d_value_sum), 0)::text
			FROM core_ingestion.rollup_deltas
			WHERE tenant_id = $1 AND definition_id = $2 AND group_value = $3
				AND event_time >= $4 AND event_time < $5
				AND ($6::pg_snapshot IS NULL OR NOT pg_visible_in_snapshot(xid, $6::pg_snapshot))
		`, def.TenantID, def.ID, s.gv, s.bstart.UTC(), bend, def.BackfillSnapshot).Scan(&dRows, &dVC, &dVS); err != nil {
			return mismatches, err
		}

		rawSumExpr := "0::numeric"
		if def.ValueType == ingestion.RollupValueTypeInt || def.ValueType == ingestion.RollupValueTypeFloat {
			rawSumExpr = fmt.Sprintf("COALESCE(SUM(%s::numeric), 0)", valCol)
		}
		rawQuery := fmt.Sprintf(`
			SELECT COUNT(*)::bigint, COUNT(%s)::bigint, %s::text
			FROM %s
			WHERE %s::text = $1 AND %s >= $2 AND %s < $3
		`, valCol, rawSumExpr, qualifiedTable, groupCol, timeCol, timeCol)
		var rRows, rVC int64
		var rVS string
		if err := tx.QueryRow(ctx, rawQuery, s.gv, s.bstart.UTC(), bend).Scan(&rRows, &rVC, &rVS); err != nil {
			return mismatches, err
		}

		if s.rows+dRows != rRows || s.vc+dVC != rVC || !numericStringsEqual(s.vs, dVS, rVS) {
			mismatches++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mismatches, err
	}

	if mismatches > 0 {
		recordRollupReconcileMismatch()
		_ = NewRollupRepository(pool).UpdateDefinitionStatus(ctx, def.TenantID, def.ID, ingestion.RollupStatusDisabled, nil)
		if cache != nil {
			cache.Invalidate(def.TenantID, def.TableName)
		}
	}
	return mismatches, nil
}

func numericStringsEqual(bucketSum, deltaSum, rawSum string) bool {
	b, ok1 := new(big.Rat).SetString(bucketSum)
	d, ok2 := new(big.Rat).SetString(deltaSum)
	r, ok3 := new(big.Rat).SetString(rawSum)
	if !ok1 || !ok2 || !ok3 {
		return false
	}
	b.Add(b, d)
	if b.Cmp(r) == 0 {
		return true
	}
	diff := new(big.Rat).Sub(b, r)
	diff.Abs(diff)
	tol := new(big.Rat).SetFrac64(1, 1_000_000_000) // 1e-9
	return diff.Cmp(tol) <= 0
}

// PruneExpiredRollupBuckets deletes expired buckets in bounded batches (LIMIT 10000).
func PruneExpiredRollupBuckets(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var total int64
	for _, res := range ingestion.RollupResolutions {
		cutoff := now.UTC().Add(-ingestion.RollupRetentionForResolution(res))
		for {
			tag, err := pool.Exec(ctx, `
				DELETE FROM core_ingestion.rollup_buckets
				WHERE ctid IN (
					SELECT ctid
					FROM core_ingestion.rollup_buckets
					WHERE resolution = $1 AND bucket_start < $2
					LIMIT 10000
				)
			`, res, cutoff)
			if err != nil {
				return total, err
			}
			n := tag.RowsAffected()
			total += n
			if n < 10000 {
				break
			}
		}
	}
	return total, nil
}

// RevalidateRollupDefinitionsAgainstModel enforces §6.2 schema change invalidation:
// disables definitions whose (table_name, group_field, time_field, value_field) were dropped,
// archived, or retyped incompatibly, and fast-forwards model_version on unrelated model changes.
func RevalidateRollupDefinitionsAgainstModel(
	ctx context.Context,
	pool *pgxpool.Pool,
	cache *RollupDefinitionCache,
	model ingestion.PublishedDataModel,
) error {
	repo := NewRollupRepository(pool)
	defs, err := repo.ListDefinitionsByTenant(ctx, model.TenantID.String())
	if err != nil {
		return err
	}
	for _, def := range defs {
		if def.Status == ingestion.RollupStatusDisabled || def.ModelVersion == model.RevisionID {
			continue
		}
		if rollupSignatureValidForModel(model, def) {
			if err := repo.FastForwardModelVersion(ctx, def.TenantID, def.ID, model.RevisionID); err != nil {
				return err
			}
		} else {
			if err := repo.UpdateDefinitionStatus(ctx, def.TenantID, def.ID, ingestion.RollupStatusDisabled, nil); err != nil {
				return err
			}
		}
		if cache != nil {
			cache.Invalidate(def.TenantID, def.TableName)
		}
	}
	return nil
}

func rollupSignatureValidForModel(model ingestion.PublishedDataModel, def ingestion.RollupDefinition) bool {
	table, ok := model.Tables[def.TableName]
	if !ok {
		return false
	}
	if _, ok := table.Fields[def.GroupField]; !ok {
		return false
	}
	tf, ok := table.Fields[def.TimeField]
	if !ok || !isTimestampDataType(tf.DataType) {
		return false
	}
	if def.ValueType == ingestion.RollupValueTypeLookupCount {
		return def.ValueField == model.RecordLookupField
	}
	vf, ok := table.Fields[def.ValueField]
	if !ok {
		return false
	}
	dt := stringsToLowerTrim(vf.DataType)
	switch def.ValueType {
	case ingestion.RollupValueTypeInt:
		return dt == "int" || dt == "integer" || dt == "int4" || dt == "int8" || dt == "bigint"
	case ingestion.RollupValueTypeFloat:
		return dt == "float" || dt == "float8" || dt == "double precision" || dt == "numeric"
	default:
		return false
	}
}

func stringsToLowerTrim(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			out = append(out, c)
		}
	}
	return string(out)
}
