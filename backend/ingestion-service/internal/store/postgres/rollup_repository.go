package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RollupRepository struct {
	pool *pgxpool.Pool
}

func NewRollupRepository(pool *pgxpool.Pool) RollupRepository {
	return RollupRepository{pool: pool}
}

func (r RollupRepository) UpsertDefinition(ctx context.Context, def ingestion.RollupDefinition) (ingestion.RollupDefinition, error) {
	const stmt = `
		INSERT INTO core_ingestion.rollup_definitions (
			tenant_id, id, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL, $10, $10)
		ON CONFLICT (tenant_id, table_name, group_field, time_field, value_field)
		DO UPDATE SET
			value_type = EXCLUDED.value_type,
			model_version = EXCLUDED.model_version,
			status = CASE
				WHEN core_ingestion.rollup_definitions.status = 'disabled' THEN 'capturing'
				ELSE core_ingestion.rollup_definitions.status
			END,
			updated_at = EXCLUDED.updated_at
		RETURNING tenant_id, id, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot::text, created_at, updated_at
	`
	now := def.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return scanRollupDefinition(r.pool.QueryRow(
		ctx, stmt,
		def.TenantID, def.ID, def.TableName, def.GroupField, def.TimeField, def.ValueField,
		string(def.ValueType), def.ModelVersion, string(def.Status), now,
	))
}

func (r RollupRepository) GetDefinition(ctx context.Context, tenantID, id string) (ingestion.RollupDefinition, error) {
	const stmt = `
		SELECT tenant_id, id, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot::text, created_at, updated_at
		FROM core_ingestion.rollup_definitions
		WHERE tenant_id = $1 AND id = $2
	`
	return scanRollupDefinition(r.pool.QueryRow(ctx, stmt, tenantID, id))
}

func (r RollupRepository) ListDefinitionsByTenant(ctx context.Context, tenantID string) ([]ingestion.RollupDefinition, error) {
	const stmt = `
		SELECT tenant_id, id, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot::text, created_at, updated_at
		FROM core_ingestion.rollup_definitions
		WHERE tenant_id = $1
		ORDER BY table_name, created_at
	`
	return r.listDefinitions(ctx, stmt, tenantID)
}

func (r RollupRepository) ListDefinitionsByTable(ctx context.Context, tenantID, tableName string) ([]ingestion.RollupDefinition, error) {
	const stmt = `
		SELECT tenant_id, id, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot::text, created_at, updated_at
		FROM core_ingestion.rollup_definitions
		WHERE tenant_id = $1 AND table_name = $2
		ORDER BY created_at
	`
	return r.listDefinitions(ctx, stmt, tenantID, tableName)
}

func (r RollupRepository) ListAllReadyDefinitions(ctx context.Context) ([]ingestion.RollupDefinition, error) {
	const stmt = `
		SELECT tenant_id, id, table_name, group_field, time_field, value_field,
			value_type, model_version, status, backfill_snapshot::text, created_at, updated_at
		FROM core_ingestion.rollup_definitions
		WHERE status = 'ready'
		ORDER BY tenant_id, id
	`
	return r.listDefinitions(ctx, stmt)
}

func (r RollupRepository) UpdateDefinitionStatus(ctx context.Context, tenantID, id string, status ingestion.RollupStatus, backfillSnapshot *string) error {
	const stmt = `
		UPDATE core_ingestion.rollup_definitions
		SET status = $3,
			backfill_snapshot = CASE WHEN $4::text IS NULL THEN backfill_snapshot ELSE $4::pg_snapshot END,
			updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
	`
	_, err := r.pool.Exec(ctx, stmt, tenantID, id, string(status), backfillSnapshot)
	return err
}

func (r RollupRepository) ResetDefinitionToCapturing(ctx context.Context, tenantID, id string) error {
	const stmt = `
		UPDATE core_ingestion.rollup_definitions
		SET status = 'capturing',
			backfill_snapshot = NULL,
			updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
	`
	_, err := r.pool.Exec(ctx, stmt, tenantID, id)
	return err
}

func (r RollupRepository) FastForwardModelVersion(ctx context.Context, tenantID, id, modelVersion string) error {
	const stmt = `
		UPDATE core_ingestion.rollup_definitions
		SET model_version = $3,
			updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
	`
	_, err := r.pool.Exec(ctx, stmt, tenantID, id, modelVersion)
	return err
}

func (r RollupRepository) CountActiveByTable(ctx context.Context, tenantID, tableName string) (int, error) {
	const stmt = `
		SELECT COUNT(*)
		FROM core_ingestion.rollup_definitions
		WHERE tenant_id = $1 AND table_name = $2 AND status <> 'disabled'
	`
	var count int
	err := r.pool.QueryRow(ctx, stmt, tenantID, tableName).Scan(&count)
	return count, err
}

func (r RollupRepository) CountDefinitionsByStatus(ctx context.Context) (map[string]int, error) {
	const stmt = `
		SELECT status, COUNT(*)
		FROM core_ingestion.rollup_definitions
		GROUP BY status
	`
	rows, err := r.pool.Query(ctx, stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}

func (r RollupRepository) CountPendingDeltas(ctx context.Context) (int64, error) {
	const stmt = `SELECT COUNT(*) FROM core_ingestion.rollup_deltas`
	var count int64
	err := r.pool.QueryRow(ctx, stmt).Scan(&count)
	return count, err
}

func (r RollupRepository) listDefinitions(ctx context.Context, stmt string, args ...any) ([]ingestion.RollupDefinition, error) {
	rows, err := r.pool.Query(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ingestion.RollupDefinition
	for rows.Next() {
		def, err := scanRollupDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, rows.Err()
}

func scanRollupDefinition(row pgx.Row) (ingestion.RollupDefinition, error) {
	var def ingestion.RollupDefinition
	var valueType string
	var status string
	err := row.Scan(
		&def.TenantID, &def.ID, &def.TableName, &def.GroupField, &def.TimeField, &def.ValueField,
		&valueType, &def.ModelVersion, &status, &def.BackfillSnapshot, &def.CreatedAt, &def.UpdatedAt,
	)
	if err != nil {
		return ingestion.RollupDefinition{}, err
	}
	def.ValueType = ingestion.RollupValueType(valueType)
	def.Status = ingestion.RollupStatus(status)
	return def, nil
}

func insertRollupDeltasTx(ctx context.Context, tx pgx.Tx, deltas []ingestion.RollupDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO core_ingestion.rollup_deltas (tenant_id, definition_id, group_value, event_time, d_rows, d_value_count, d_value_sum) VALUES `)
	args := make([]any, 0, len(deltas)*7)
	for i, d := range deltas {
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i*7 + 1
		fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d, $%d::numeric)",
			base, base+1, base+2, base+3, base+4, base+5, base+6)
		sumStr := d.DValueSum
		if sumStr == "" {
			sumStr = "0"
		}
		args = append(args, d.TenantID, d.DefinitionID, d.GroupValue, d.EventTime.UTC(), d.DRows, d.DValueCount, sumStr)
	}
	_, err := tx.Exec(ctx, sb.String(), args...)
	return err
}
