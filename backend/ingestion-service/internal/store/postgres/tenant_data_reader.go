package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type readOnlyTxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type TenantDataReader struct {
	db               txExecutor
	statementTimeout time.Duration
	rollupCache      *RollupDefinitionCache
	rollupsEnabled   bool
	rollupShadowMode bool
}

func NewTenantDataReader(db txExecutor) TenantDataReader {
	return TenantDataReader{db: db}
}

func (r TenantDataReader) WithStatementTimeout(timeout time.Duration) TenantDataReader {
	r.statementTimeout = timeout
	return r
}

func (r TenantDataReader) WithRollups(cache *RollupDefinitionCache, enabled, shadowMode bool) TenantDataReader {
	r.rollupCache = cache
	r.rollupsEnabled = enabled
	r.rollupShadowMode = shadowMode
	return r
}

func (r TenantDataReader) GetRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType, objectID string) (map[string]any, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return nil, fmt.Errorf("object type %s is not available", objectType)
	}
	schemaName := schemaNameForModel(model)
	query := fmt.Sprintf(`SELECT * FROM %s WHERE %s = $1 LIMIT 1`, sanitizeIdentifier(schemaName, table.Name), sanitizeIdentifier(model.RecordLookupField))

	rows, err := r.db.Query(ctx, query, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(row))
	for key, value := range row {
		switch typed := value.(type) {
		case uuid.UUID:
			out[key] = typed.String()
		default:
			out[key] = typed
		}
	}
	return out, nil
}

func (r TenantDataReader) ListRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType string, limit int) ([]map[string]any, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return nil, fmt.Errorf("object type %s is not available", objectType)
	}
	if limit <= 0 {
		limit = 100
	}
	schemaName := schemaNameForModel(model)
	query := fmt.Sprintf(`SELECT * FROM %s ORDER BY %s DESC LIMIT $1`, sanitizeIdentifier(schemaName, table.Name), sanitizeIdentifier("updated_at"))
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		row, err := pgx.RowToMap(rows)
		if err != nil {
			return nil, err
		}
		out := make(map[string]any, len(row))
		for key, value := range row {
			switch typed := value.(type) {
			case uuid.UUID:
				out[key] = typed.String()
			default:
				out[key] = typed
			}
		}
		if _, ok := out[model.RecordLookupField]; !ok {
			out[model.RecordLookupField] = strings.TrimSpace(fmt.Sprint(out["object_id"]))
		}
		items = append(items, out)
	}
	return items, rows.Err()
}

func (r TenantDataReader) QueryRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType, fieldName, value string, limit int) ([]map[string]any, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return nil, fmt.Errorf("object type %s is not available", objectType)
	}
	if _, ok := table.Fields[fieldName]; !ok && fieldName != model.RecordLookupField {
		return nil, fmt.Errorf("field %s is not available on object type %s", fieldName, objectType)
	}
	if limit <= 0 {
		limit = 100
	}
	schemaName := schemaNameForModel(model)
	query := fmt.Sprintf(`SELECT * FROM %s WHERE %s = $1 ORDER BY %s DESC LIMIT $2`, sanitizeIdentifier(schemaName, table.Name), sanitizeIdentifier(fieldName), sanitizeIdentifier("updated_at"))
	rows, err := r.db.Query(ctx, query, value, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		row, err := pgx.RowToMap(rows)
		if err != nil {
			return nil, err
		}
		out := make(map[string]any, len(row))
		for key, raw := range row {
			switch typed := raw.(type) {
			case uuid.UUID:
				out[key] = typed.String()
			default:
				out[key] = typed
			}
		}
		if _, ok := out[model.RecordLookupField]; !ok {
			out[model.RecordLookupField] = strings.TrimSpace(fmt.Sprint(out["object_id"]))
		}
		items = append(items, out)
	}
	return items, rows.Err()
}

func (r TenantDataReader) AggregateRecords(ctx context.Context, model ingestion.PublishedDataModel, query ingestion.AggregateQuery) (any, error) {
	startedAt := time.Now()
	table, ok := model.Tables[query.ObjectType]
	if !ok {
		return nil, fmt.Errorf("object type %s is not available", query.ObjectType)
	}

	fieldName := strings.TrimSpace(query.Field)
	if fieldName == "" {
		return nil, fmt.Errorf("field is required")
	}
	if _, ok := table.Fields[fieldName]; !ok && fieldName != model.RecordLookupField {
		return nil, fmt.Errorf("field %s is not available on object type %s", fieldName, query.ObjectType)
	}

	aggregateExpr, err := buildAggregateExpression(strings.TrimSpace(query.Aggregate), fieldName)
	if err != nil {
		return nil, err
	}

	var rollupCand *RollupQueryCandidate
	if (r.rollupsEnabled || r.rollupShadowMode) && r.rollupCache != nil {
		if defs, defErr := r.rollupCache.GetActiveForTable(ctx, r.db, model.TenantID.String(), table.Name); defErr == nil && len(defs) > 0 {
			cand, fallbackReason := checkRollupEligibility(model, table, query, defs, time.Now().UTC())
			if fallbackReason == "" {
				rollupCand = &cand
				if r.rollupsEnabled && !r.rollupShadowMode {
					rollupSQL, rollupArgs, buildErr := buildRollupUnionSQL(model, table, cand)
					if buildErr == nil {
						rollupVal, rollupErr := executeRollupQuery(ctx, r.db, r.statementTimeout, rollupSQL, rollupArgs)
						if rollupErr == nil {
							recordAggregateSQLDuration("rollup", time.Since(startedAt))
							return rollupVal, nil
						}
						slog.Default().Warn("rollup aggregate query failed; falling back to raw",
							"tenant_id", model.TenantID,
							"object_type", query.ObjectType,
							"definition_id", cand.Definition.ID,
							"error", rollupErr,
						)
					}
				}
			} else {
				recordRollupPathFallback(fallbackReason)
			}
		}
	}

	whereSQL := ""
	args := []any{}
	if query.Filter != nil {
		built, err := buildAggregateFilterSQL(model, table, *query.Filter, &args)
		if err != nil {
			return nil, err
		}
		if built != "" {
			whereSQL = " WHERE " + built
		}
	}

	schemaName := schemaNameForModel(model)
	castedExpr := castRawAggregateExpr(strings.TrimSpace(query.Aggregate), aggregateExpr, table.Fields[fieldName].DataType)
	sql := fmt.Sprintf(`SELECT %s FROM %s%s`, castedExpr, sanitizeIdentifier(schemaName, table.Name), whereSQL)
	var value any
	var queryErr error
	if r.statementTimeout > 0 {
		if beginner, ok := r.db.(readOnlyTxBeginner); ok {
			tx, txErr := beginner.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if txErr != nil {
				return nil, txErr
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, execErr := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", max(1, int(r.statementTimeout.Milliseconds())))); execErr != nil {
				return nil, execErr
			}
			queryErr = tx.QueryRow(ctx, sql, args...).Scan(&value)
			if queryErr == nil {
				queryErr = tx.Commit(ctx)
			}
		} else {
			queryErr = r.db.QueryRow(ctx, sql, args...).Scan(&value)
		}
	} else {
		queryErr = r.db.QueryRow(ctx, sql, args...).Scan(&value)
	}
	if queryErr != nil {
		slog.Default().Warn("aggregate query execution failed",
			"tenant_id", model.TenantID,
			"object_type", query.ObjectType,
			"aggregate", strings.ToLower(strings.TrimSpace(query.Aggregate)),
			"field", fieldName,
			"filter_present", query.Filter != nil,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"error", queryErr,
		)
		return nil, queryErr
	}
	slog.Default().Info("aggregate query executed",
		"tenant_id", model.TenantID,
		"object_type", query.ObjectType,
		"aggregate", strings.ToLower(strings.TrimSpace(query.Aggregate)),
		"field", fieldName,
		"filter_present", query.Filter != nil,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	recordAggregateSQLDuration("raw", time.Since(startedAt))

	if r.rollupShadowMode && rollupCand != nil {
		if rollupSQL, rollupArgs, buildErr := buildRollupUnionSQL(model, table, *rollupCand); buildErr == nil {
			rollupStarted := time.Now()
			if rollupVal, rollupErr := executeRollupQuery(ctx, r.db, r.statementTimeout, rollupSQL, rollupArgs); rollupErr == nil {
				recordAggregateSQLDuration("rollup_shadow", time.Since(rollupStarted))
				if !compareRollupShadowValues(rollupCand.Aggregate, value, rollupVal) {
					recordRollupShadowMismatch()
					slog.Default().Warn("rollup shadow mismatch",
						"tenant_id", model.TenantID,
						"definition_id", rollupCand.Definition.ID,
						"aggregate", rollupCand.Aggregate,
						"raw_value", value,
						"rollup_value", rollupVal,
					)
				}
			}
		}
	}

	return value, nil
}

func castRawAggregateExpr(aggName, expr, dataType string) string {
	switch strings.ToLower(strings.TrimSpace(aggName)) {
	case "sum":
		dt := strings.ToLower(strings.TrimSpace(dataType))
		if dt == "int" || dt == "integer" || dt == "int4" || dt == "int8" || dt == "bigint" {
			return "(" + expr + ")::bigint"
		}
		if dt == "float" || dt == "float8" || dt == "double precision" || dt == "numeric" {
			return "(" + expr + ")::double precision"
		}
		return expr
	case "avg":
		return "(" + expr + ")::double precision"
	default:
		return expr
	}
}

func buildAggregateExpression(name, fieldName string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "count":
		return fmt.Sprintf("COUNT(%s)", sanitizeIdentifier(fieldName)), nil
	case "count_distinct":
		return fmt.Sprintf("COUNT(DISTINCT %s)", sanitizeIdentifier(fieldName)), nil
	case "sum":
		return fmt.Sprintf("COALESCE(SUM(%s), 0)", sanitizeIdentifier(fieldName)), nil
	case "avg":
		return fmt.Sprintf("AVG(%s)", sanitizeIdentifier(fieldName)), nil
	case "min":
		return fmt.Sprintf("MIN(%s)", sanitizeIdentifier(fieldName)), nil
	case "max":
		return fmt.Sprintf("MAX(%s)", sanitizeIdentifier(fieldName)), nil
	default:
		return "", fmt.Errorf("aggregate %q is not supported", name)
	}
}

func buildAggregateExpressionForField(name, fieldName, dataType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "count":
		return fmt.Sprintf("COUNT(%s)", sanitizeIdentifier(fieldName)), nil
	case "count_distinct":
		return fmt.Sprintf("COUNT(DISTINCT %s)", sanitizeIdentifier(fieldName)), nil
	case "sum":
		dt := strings.ToLower(strings.TrimSpace(dataType))
		if dt == "int" || dt == "integer" || dt == "int4" || dt == "int8" || dt == "bigint" {
			return fmt.Sprintf("COALESCE(SUM(%s), 0)::bigint", sanitizeIdentifier(fieldName)), nil
		}
		if dt == "float" || dt == "float8" || dt == "double precision" || dt == "numeric" {
			return fmt.Sprintf("COALESCE(SUM(%s), 0)::double precision", sanitizeIdentifier(fieldName)), nil
		}
		return fmt.Sprintf("COALESCE(SUM(%s), 0)", sanitizeIdentifier(fieldName)), nil
	case "avg":
		return fmt.Sprintf("AVG(%s)::double precision", sanitizeIdentifier(fieldName)), nil
	case "min":
		return fmt.Sprintf("MIN(%s)", sanitizeIdentifier(fieldName)), nil
	case "max":
		return fmt.Sprintf("MAX(%s)", sanitizeIdentifier(fieldName)), nil
	default:
		return "", fmt.Errorf("aggregate %q is not supported", name)
	}
}

func buildAggregateFilterSQL(model ingestion.PublishedDataModel, table ingestion.ObjectSchema, filter ingestion.AggregateFilter, args *[]any) (string, error) {
	switch strings.ToLower(strings.TrimSpace(filter.Kind)) {
	case "", ingestion.AggregateFilterKindGroup:
		op := strings.ToLower(strings.TrimSpace(filter.Operator))
		if op == "" {
			op = "and"
		}
		switch op {
		case "and", "or":
			if len(filter.Children) == 0 {
				return "", nil
			}
			parts := make([]string, 0, len(filter.Children))
			for _, child := range filter.Children {
				part, err := buildAggregateFilterSQL(model, table, child, args)
				if err != nil {
					return "", err
				}
				if part != "" {
					parts = append(parts, "("+part+")")
				}
			}
			if len(parts) == 0 {
				return "", nil
			}
			return strings.Join(parts, " "+strings.ToUpper(op)+" "), nil
		case "not":
			if len(filter.Children) != 1 {
				return "", fmt.Errorf("filter group operator %q expects exactly one child", filter.Operator)
			}
			part, err := buildAggregateFilterSQL(model, table, filter.Children[0], args)
			if err != nil {
				return "", err
			}
			if part == "" {
				return "", nil
			}
			return "NOT (" + part + ")", nil
		default:
			return "", fmt.Errorf("filter group operator %q is not supported", filter.Operator)
		}
	case ingestion.AggregateFilterKindPredicate:
		return buildAggregatePredicateSQL(model, table, filter, args)
	default:
		return "", fmt.Errorf("filter kind %q is not supported", filter.Kind)
	}
}

func buildAggregatePredicateSQL(model ingestion.PublishedDataModel, table ingestion.ObjectSchema, filter ingestion.AggregateFilter, args *[]any) (string, error) {
	fieldName := strings.TrimSpace(filter.Field)
	if fieldName == "" {
		return "", fmt.Errorf("filter field is required")
	}
	if _, ok := table.Fields[fieldName]; !ok && fieldName != model.RecordLookupField {
		return "", fmt.Errorf("field %s is not available on object type %s", fieldName, table.Name)
	}
	column := sanitizeIdentifier(fieldName)
	switch strings.ToLower(strings.TrimSpace(filter.Op)) {
	case "eq":
		*args = append(*args, filter.Value)
		return fmt.Sprintf("%s = $%d", column, len(*args)), nil
	case "neq":
		*args = append(*args, filter.Value)
		return fmt.Sprintf("%s <> $%d", column, len(*args)), nil
	case "gt":
		*args = append(*args, filter.Value)
		return fmt.Sprintf("%s > $%d", column, len(*args)), nil
	case "gte":
		*args = append(*args, filter.Value)
		return fmt.Sprintf("%s >= $%d", column, len(*args)), nil
	case "lt":
		*args = append(*args, filter.Value)
		return fmt.Sprintf("%s < $%d", column, len(*args)), nil
	case "lte":
		*args = append(*args, filter.Value)
		return fmt.Sprintf("%s <= $%d", column, len(*args)), nil
	case "in":
		items, ok := filter.Value.([]any)
		if !ok || len(items) == 0 {
			return "", fmt.Errorf("in filter expects a non-empty list value")
		}
		placeholders := make([]string, 0, len(items))
		for _, item := range items {
			*args = append(*args, item)
			placeholders = append(placeholders, "$"+strconv.Itoa(len(*args)))
		}
		return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), nil
	case "is_null":
		return fmt.Sprintf("%s IS NULL", column), nil
	case "is_not_null":
		return fmt.Sprintf("%s IS NOT NULL", column), nil
	case "starts_with":
		*args = append(*args, filter.Value)
		return fmt.Sprintf("%s LIKE ($%d || '%%')", column, len(*args)), nil
	default:
		return "", fmt.Errorf("filter operator %q is not supported", filter.Op)
	}
}
