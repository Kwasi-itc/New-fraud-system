package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

type TenantDataWriter struct {
	db txExecutor
}

func NewTenantDataWriter(db txExecutor) TenantDataWriter {
	return TenantDataWriter{db: db}
}

func (w TenantDataWriter) UpsertRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType string, record map[string]any, mode ingestion.Mode, now time.Time) (string, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return "", fmt.Errorf("object type %s is not available for ingestion", objectType)
	}
	_ = mode

	objectID, _ := record[model.RecordLookupField].(string)
	schemaName := tenantSchemaName(model.TenantID)
	exists, err := w.objectExists(ctx, schemaName, table.Name, objectID)
	if err != nil {
		return "", err
	}

	columnNames := []string{"id", model.RecordLookupField, "updated_at"}
	values := []any{uuid.New(), objectID, now}
	updateAssignments := []string{fmt.Sprintf("%s = EXCLUDED.%s", sanitizeIdentifier("updated_at"), sanitizeIdentifier("updated_at"))}

	fieldNames := make([]string, 0, len(record))
	for fieldName := range record {
		if fieldName == model.RecordLookupField {
			continue
		}
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		columnNames = append(columnNames, fieldName)
		values = append(values, record[fieldName])
		updateAssignments = append(updateAssignments, fmt.Sprintf("%s = EXCLUDED.%s", sanitizeIdentifier(fieldName), sanitizeIdentifier(fieldName)))
	}

	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	insertColumns := make([]string, len(columnNames))
	for i, columnName := range columnNames {
		insertColumns[i] = sanitizeIdentifier(columnName)
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (%s)
		VALUES (%s)
		ON CONFLICT (%s) DO UPDATE SET %s
	`,
		sanitizeIdentifier(schemaName, table.Name),
		strings.Join(insertColumns, ", "),
		strings.Join(placeholders, ", "),
		sanitizeIdentifier(model.RecordLookupField),
		strings.Join(updateAssignments, ", "),
	)
	if _, err := w.db.Exec(ctx, query, values...); err != nil {
		return "", err
	}

	if exists {
		return "updated", nil
	}
	return "created", nil
}

func (w TenantDataWriter) objectExists(ctx context.Context, schemaName, tableName, objectID string) (bool, error) {
	var exists bool
	query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE object_id = $1)`, sanitizeIdentifier(schemaName, tableName))
	if err := w.db.QueryRow(ctx, query, objectID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func tenantSchemaName(tenantID uuid.UUID) string {
	return "tenant_" + strings.ReplaceAll(tenantID.String(), "-", "")
}
