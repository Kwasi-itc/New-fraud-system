package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type TenantDataReader struct {
	db txExecutor
}

func NewTenantDataReader(db txExecutor) TenantDataReader {
	return TenantDataReader{db: db}
}

func (r TenantDataReader) GetRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType, objectID string) (map[string]any, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return nil, fmt.Errorf("object type %s is not available", objectType)
	}
	schemaName := tenantSchemaName(model.TenantID)
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
	schemaName := tenantSchemaName(model.TenantID)
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
	schemaName := tenantSchemaName(model.TenantID)
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
