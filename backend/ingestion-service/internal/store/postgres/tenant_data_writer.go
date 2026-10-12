package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

	objectID, _ := record[model.RecordLookupField].(string)
	schemaName := schemaNameForModel(model)
	if mode == ingestion.ModePatch {
		err := w.patchRecord(ctx, schemaName, table.Name, model.RecordLookupField, objectID, record, now)
		if err == nil {
			return "updated", nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}

	columnNames := []string{"id", model.RecordLookupField, "updated_at"}
	values := []any{uuid.New(), objectID, now}

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
		ON CONFLICT (%s) DO NOTHING
		RETURNING id
	`,
		sanitizeIdentifier(schemaName, table.Name),
		strings.Join(insertColumns, ", "),
		strings.Join(placeholders, ", "),
		sanitizeIdentifier(model.RecordLookupField),
	)
	var insertedID uuid.UUID
	err := w.db.QueryRow(ctx, query, values...).Scan(&insertedID)
	if err == nil {
		return "created", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// A concurrent insert is visible to this next READ COMMITTED statement.
	// UPDATE RETURNING prevents reporting success if the row disappeared.
	if err := w.patchRecord(ctx, schemaName, table.Name, model.RecordLookupField, objectID, record, now); err != nil {
		return "", err
	}
	return "updated", nil
}

func (w TenantDataWriter) patchRecord(ctx context.Context, schemaName, tableName, lookupField, objectID string, record map[string]any, now time.Time) error {
	assignments := []string{fmt.Sprintf("%s = $1", sanitizeIdentifier("updated_at"))}
	values := []any{now}

	fieldNames := make([]string, 0, len(record))
	for fieldName := range record {
		if fieldName == lookupField {
			continue
		}
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		values = append(values, record[fieldName])
		assignments = append(assignments, fmt.Sprintf("%s = $%d", sanitizeIdentifier(fieldName), len(values)))
	}
	values = append(values, objectID)

	query := fmt.Sprintf(`
		UPDATE %s
		SET %s
		WHERE %s = $%d
		RETURNING id
	`,
		sanitizeIdentifier(schemaName, tableName),
		strings.Join(assignments, ", "),
		sanitizeIdentifier(lookupField),
		len(values),
	)
	var id uuid.UUID
	return w.db.QueryRow(ctx, query, values...).Scan(&id)
}

func schemaNameForModel(model ingestion.PublishedDataModel) string {
	if model.SchemaName != "" {
		return model.SchemaName
	}
	return tenantSchemaName(model.TenantID)
}

func tenantSchemaName(tenantID uuid.UUID) string {
	return "tenant_" + strings.ReplaceAll(tenantID.String(), "-", "")
}
