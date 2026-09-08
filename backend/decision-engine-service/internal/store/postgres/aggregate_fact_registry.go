package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type AggregateFactRegistry struct {
	db queryable
}

func NewAggregateFactRegistry(db queryable) AggregateFactRegistry {
	return AggregateFactRegistry{db: db}
}

func (r AggregateFactRegistry) SyncScenario(ctx context.Context, tenantID, scenarioID, iterationID string, definitions []ports.AggregateFactDefinition) error {
	txSource, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return fmt.Errorf("aggregate fact registry does not support transactions")
	}
	tx, err := txSource.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	oldTables, err := boundFactTables(ctx, tx, tenantID, scenarioID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	touched := oldTables
	desiredDefinitionIDs := make([]uuid.UUID, 0, len(definitions))
	for _, definition := range definitions {
		backfillRequired, err := tenantTableHasRows(ctx, tx, tenantID, definition.TableName)
		if err != nil {
			return err
		}
		definitionID := uuid.NewString()
		coverageToken := uuid.NewString()
		err = tx.QueryRow(ctx, `
			INSERT INTO core.aggregate_fact_definitions (
				id, tenant_id, table_id, table_name, signature, dimension_fields,
				event_time_field, measure_field, needs_sum, needs_count,
				max_window_seconds, minute_enabled, backfill_required, coverage_token, version,
				created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1,$15,$15)
			ON CONFLICT (tenant_id, signature) DO UPDATE SET
				needs_sum = core.aggregate_fact_definitions.needs_sum OR EXCLUDED.needs_sum,
				needs_count = core.aggregate_fact_definitions.needs_count OR EXCLUDED.needs_count,
				max_window_seconds = GREATEST(core.aggregate_fact_definitions.max_window_seconds, EXCLUDED.max_window_seconds),
				minute_enabled = core.aggregate_fact_definitions.minute_enabled OR EXCLUDED.minute_enabled,
				backfill_required = CASE WHEN
					(NOT core.aggregate_fact_definitions.needs_sum AND EXCLUDED.needs_sum)
					OR (NOT core.aggregate_fact_definitions.minute_enabled AND EXCLUDED.minute_enabled)
					OR NOT EXISTS (
						SELECT 1
						FROM core.aggregate_fact_bindings active_binding
						JOIN core.scenarios active_scenario
						  ON active_scenario.id = active_binding.scenario_id
						 AND active_scenario.live_iteration_id = active_binding.iteration_id
						WHERE active_binding.definition_id = core.aggregate_fact_definitions.id
					)
					THEN EXCLUDED.backfill_required
					ELSE core.aggregate_fact_definitions.backfill_required
				END,
				coverage_token = CASE WHEN
					(NOT core.aggregate_fact_definitions.needs_sum AND EXCLUDED.needs_sum)
					OR (NOT core.aggregate_fact_definitions.minute_enabled AND EXCLUDED.minute_enabled)
					OR NOT EXISTS (
						SELECT 1
						FROM core.aggregate_fact_bindings active_binding
						JOIN core.scenarios active_scenario
						  ON active_scenario.id = active_binding.scenario_id
						 AND active_scenario.live_iteration_id = active_binding.iteration_id
						WHERE active_binding.definition_id = core.aggregate_fact_definitions.id
					)
					THEN EXCLUDED.coverage_token
					ELSE core.aggregate_fact_definitions.coverage_token
				END,
				version = CASE WHEN
					(NOT core.aggregate_fact_definitions.needs_sum AND EXCLUDED.needs_sum)
					OR (NOT core.aggregate_fact_definitions.minute_enabled AND EXCLUDED.minute_enabled)
					OR NOT EXISTS (
						SELECT 1
						FROM core.aggregate_fact_bindings active_binding
						JOIN core.scenarios active_scenario
						  ON active_scenario.id = active_binding.scenario_id
						 AND active_scenario.live_iteration_id = active_binding.iteration_id
						WHERE active_binding.definition_id = core.aggregate_fact_definitions.id
					)
					THEN core.aggregate_fact_definitions.version + 1
					ELSE core.aggregate_fact_definitions.version
				END,
				updated_at = EXCLUDED.updated_at
			RETURNING id
		`, definitionID, tenantID, definition.TableID, definition.TableName, definition.Signature,
			definition.DimensionFields, definition.EventTimeField, definition.MeasureField,
			definition.NeedsSum, definition.NeedsCount, definition.MaxWindowSeconds,
			definition.MinuteEnabled, backfillRequired, coverageToken, now).Scan(&definitionID)
		if err != nil {
			return err
		}
		parsedDefinitionID, err := uuid.Parse(definitionID)
		if err != nil {
			return fmt.Errorf("parse aggregate fact definition id: %w", err)
		}
		desiredDefinitionIDs = append(desiredDefinitionIDs, parsedDefinitionID)
		if _, err := tx.Exec(ctx, `
			INSERT INTO core.aggregate_fact_bindings (
				tenant_id, scenario_id, iteration_id, definition_id, created_at
			) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT DO NOTHING
		`, tenantID, scenarioID, iterationID, definitionID, now); err != nil {
			return err
		}
		touched[definition.TableName] = struct{}{}
	}
	if len(desiredDefinitionIDs) == 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM core.aggregate_fact_bindings
			WHERE tenant_id = $1 AND scenario_id = $2 AND iteration_id = $3
		`, tenantID, scenarioID, iterationID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			DELETE FROM core.aggregate_fact_bindings
			WHERE tenant_id = $1 AND scenario_id = $2
			  AND iteration_id = $3
			  AND NOT (definition_id = ANY($4::uuid[]))
		`, tenantID, scenarioID, iterationID, desiredDefinitionIDs); err != nil {
			return err
		}
	}
	for tableName := range touched {
		if err := bumpFactRegistryVersion(ctx, tx, tenantID, tableName, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r AggregateFactRegistry) RemoveScenario(ctx context.Context, tenantID, scenarioID string) error {
	txSource, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return fmt.Errorf("aggregate fact registry does not support transactions")
	}
	tx, err := txSource.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tables, err := boundFactTables(ctx, tx, tenantID, scenarioID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM core.aggregate_fact_bindings WHERE tenant_id = $1 AND scenario_id = $2`, tenantID, scenarioID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for tableName := range tables {
		if err := bumpFactRegistryVersion(ctx, tx, tenantID, tableName, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r AggregateFactRegistry) ListActive(ctx context.Context, tenantID, tableName string) ([]ports.AggregateFactDefinition, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT d.id, d.tenant_id, d.table_id, d.table_name, d.signature,
		       d.dimension_fields, d.event_time_field, d.measure_field,
		       d.needs_sum, d.needs_count, d.max_window_seconds,
		       d.minute_enabled, d.backfill_required, d.coverage_token, d.version,
		       COALESCE(v.version, 0)
		FROM core.aggregate_fact_definitions d
		JOIN core.aggregate_fact_bindings b ON b.definition_id = d.id AND b.tenant_id = d.tenant_id
		JOIN core.scenarios s ON s.id = b.scenario_id AND s.live_iteration_id = b.iteration_id
		LEFT JOIN core.aggregate_fact_registry_versions v
		  ON v.tenant_id = d.tenant_id AND v.table_name = d.table_name
		WHERE d.tenant_id = $1 AND d.table_name = $2
		ORDER BY d.signature
	`, tenantID, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ports.AggregateFactDefinition, 0)
	for rows.Next() {
		var item ports.AggregateFactDefinition
		if err := rows.Scan(&item.ID, &item.TenantID, &item.TableID, &item.TableName,
			&item.Signature, &item.DimensionFields, &item.EventTimeField, &item.MeasureField,
			&item.NeedsSum, &item.NeedsCount, &item.MaxWindowSeconds, &item.MinuteEnabled,
			&item.BackfillRequired, &item.CoverageToken, &item.Version, &item.RegistryVersion); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func boundFactTables(ctx context.Context, tx pgx.Tx, tenantID, scenarioID string) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT d.table_name
		FROM core.aggregate_fact_bindings b
		JOIN core.aggregate_fact_definitions d ON d.id = b.definition_id
		WHERE b.tenant_id = $1 AND b.scenario_id = $2
	`, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		out[table] = struct{}{}
	}
	return out, rows.Err()
}

func tenantTableHasRows(ctx context.Context, tx pgx.Tx, tenantID, tableName string) (bool, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil {
		return false, fmt.Errorf("parse tenant id: %w", err)
	}
	schema := "tenant_" + strings.ReplaceAll(parsed.String(), "-", "")
	qualified := sanitizeIdentifier(schema, tableName)
	var relation *string
	if err := tx.QueryRow(ctx, `SELECT to_regclass($1)::text`, schema+"."+tableName).Scan(&relation); err != nil {
		return false, err
	}
	if relation == nil {
		return false, nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s LIMIT 1)`, qualified)).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func bumpFactRegistryVersion(ctx context.Context, tx pgx.Tx, tenantID, tableName string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO core.aggregate_fact_registry_versions (tenant_id, table_name, version, updated_at)
		VALUES ($1,$2,1,$3)
		ON CONFLICT (tenant_id, table_name) DO UPDATE SET
			version = core.aggregate_fact_registry_versions.version + 1,
			updated_at = EXCLUDED.updated_at
	`, tenantID, tableName, now)
	return err
}

var _ ports.AggregateFactRegistry = AggregateFactRegistry{}
