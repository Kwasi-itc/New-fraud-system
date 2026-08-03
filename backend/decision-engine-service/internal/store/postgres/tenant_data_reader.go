package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type txExecutor interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type TenantDataReader struct {
	db              txExecutor
	dataModelReader ports.DataModelReader
}

func NewTenantDataReader(db txExecutor, dataModelReader ports.DataModelReader) TenantDataReader {
	return TenantDataReader{db: db, dataModelReader: dataModelReader}
}

func (r TenantDataReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	model, table, schemaName, err := r.resolveTable(ctx, tenantID, objectType)
	if err != nil {
		return ports.TenantRecord{}, err
	}

	query := fmt.Sprintf(
		`SELECT to_jsonb(t) FROM %s AS t WHERE %s = $1 LIMIT 1`,
		sanitizeIdentifier(schemaName, table.Name),
		sanitizeIdentifier(model.RecordLookupField),
	)
	rows, err := r.db.Query(ctx, query, objectID)
	if err != nil {
		return ports.TenantRecord{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ports.TenantRecord{}, err
		}
		return ports.TenantRecord{}, pgx.ErrNoRows
	}

	record, err := recordFromRow(rows, model.RecordLookupField, objectType)
	if err != nil {
		return ports.TenantRecord{}, err
	}
	return record, rows.Err()
}

func (r TenantDataReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	model, table, schemaName, err := r.resolveTable(ctx, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(
		`SELECT to_jsonb(t) FROM %s AS t ORDER BY %s DESC LIMIT $1`,
		sanitizeIdentifier(schemaName, table.Name),
		sanitizeIdentifier("updated_at"),
	)
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectRecords(rows, model.RecordLookupField, objectType)
}

func (r TenantDataReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	model, table, schemaName, err := r.resolveTable(ctx, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	if _, ok := table.Fields[fieldName]; !ok && fieldName != model.RecordLookupField {
		return nil, fmt.Errorf("field %s is not available on object type %s", fieldName, objectType)
	}
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(
		`SELECT to_jsonb(t) FROM %s AS t WHERE %s = $1 ORDER BY %s DESC LIMIT $2`,
		sanitizeIdentifier(schemaName, table.Name),
		sanitizeIdentifier(fieldName),
		sanitizeIdentifier("updated_at"),
	)
	rows, err := r.db.Query(ctx, query, value, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectRecords(rows, model.RecordLookupField, objectType)
}

func (r TenantDataReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	model, table, schemaName, err := r.resolveTable(ctx, tenantID, query.ObjectType)
	if err != nil {
		return nil, err
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

	sql := fmt.Sprintf(`SELECT %s FROM %s%s`, aggregateExpr, sanitizeIdentifier(schemaName, table.Name), whereSQL)
	var value any
	if err := r.db.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func (r TenantDataReader) resolveTable(ctx context.Context, tenantID, objectType string) (ports.TenantModel, ports.TenantModelTable, string, error) {
	model, err := r.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return ports.TenantModel{}, ports.TenantModelTable{}, "", err
	}
	table, ok := model.Tables[objectType]
	if !ok {
		return ports.TenantModel{}, ports.TenantModelTable{}, "", fmt.Errorf("object type %s is not available", objectType)
	}
	schemaName, err := tenantSchemaName(tenantID)
	if err != nil {
		return ports.TenantModel{}, ports.TenantModelTable{}, "", err
	}
	return model, table, schemaName, nil
}

func tenantSchemaName(tenantID string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil {
		return "", fmt.Errorf("parse tenant id: %w", err)
	}
	return "tenant_" + strings.ReplaceAll(parsed.String(), "-", ""), nil
}

func recordFromRow(rows pgx.Rows, recordLookupField, objectType string) (ports.TenantRecord, error) {
	var payload []byte
	if err := rows.Scan(&payload); err != nil {
		return ports.TenantRecord{}, err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(payload, &fields); err != nil {
		return ports.TenantRecord{}, err
	}
	return ports.TenantRecord{
		ObjectID:   strings.TrimSpace(fmt.Sprint(fields[recordLookupField])),
		ObjectType: objectType,
		Fields:     fields,
	}, nil
}

func collectRecords(rows pgx.Rows, recordLookupField, objectType string) ([]ports.TenantRecord, error) {
	var records []ports.TenantRecord
	for rows.Next() {
		record, err := recordFromRow(rows, recordLookupField, objectType)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
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

func buildAggregateFilterSQL(model ports.TenantModel, table ports.TenantModelTable, filter ports.AggregateFilter, args *[]any) (string, error) {
	switch strings.ToLower(strings.TrimSpace(filter.Kind)) {
	case "", "group":
		op := strings.ToLower(strings.TrimSpace(filter.Operator))
		if op == "" {
			op = "and"
		}
		switch op {
		case "and":
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
		default:
			return "", fmt.Errorf("filter group operator %q is not supported", filter.Operator)
		}
	case "predicate":
		return buildAggregatePredicateSQL(model, table, filter, args)
	default:
		return "", fmt.Errorf("filter kind %q is not supported", filter.Kind)
	}
}

func buildAggregatePredicateSQL(model ports.TenantModel, table ports.TenantModelTable, filter ports.AggregateFilter, args *[]any) (string, error) {
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
	default:
		return "", fmt.Errorf("filter operator %q is not supported", filter.Op)
	}
}
