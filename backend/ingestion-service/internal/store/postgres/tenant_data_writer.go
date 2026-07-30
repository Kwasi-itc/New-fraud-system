package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"

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
	schemaName := tenantSchemaName(model.TenantID)
	if strings.TrimSpace(model.PhysicalSchemaName) != "" {
		schemaName = model.PhysicalSchemaName
	}
	if err := w.lockObject(ctx, model.TenantID, table.ID, objectID); err != nil {
		return "", err
	}
	definitions := model.MaintainedBucketsForTable(table.ID, now)
	exists, oldValues, err := w.readExistingBucketValues(
		ctx,
		schemaName,
		table.Name,
		model.RecordLookupField,
		objectID,
		definitions,
	)
	if err != nil {
		return "", err
	}
	if exists {
		if err := ensureLogicalBucketTimestampsImmutable(definitions, oldValues, record); err != nil {
			return "", err
		}
	}
	if exists && mode == ingestion.ModePatch {
		if err := w.patchRecord(ctx, schemaName, table.Name, model.RecordLookupField, objectID, record, now); err != nil {
			return "", err
		}
		if err := w.bumpAffectedBuckets(ctx, model, table, definitions, oldValues, record, true, now); err != nil {
			return "", err
		}
		return "updated", nil
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
	if err := w.bumpAffectedBuckets(ctx, model, table, definitions, oldValues, record, exists, now); err != nil {
		return "", err
	}

	if exists {
		return "updated", nil
	}
	return "created", nil
}

func ensureLogicalBucketTimestampsImmutable(
	definitions []ingestion.LogicalBucketDefinition,
	oldValues, newRecord map[string]any,
) error {
	for _, definition := range definitions {
		newValue, provided := newRecord[definition.TimestampFieldName]
		if !provided {
			continue
		}
		oldTime, oldOK := timestampValue(oldValues[definition.TimestampFieldName])
		newTime, newOK := timestampValue(newValue)
		if !oldOK || !newOK || !oldTime.Equal(newTime) {
			return fmt.Errorf(
				"logical bucket timestamp %s cannot be edited while the definition is active",
				definition.TimestampFieldName,
			)
		}
	}
	return nil
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
	`,
		sanitizeIdentifier(schemaName, tableName),
		strings.Join(assignments, ", "),
		sanitizeIdentifier(lookupField),
		len(values),
	)
	_, err := w.db.Exec(ctx, query, values...)
	return err
}

func (w TenantDataWriter) lockObject(ctx context.Context, tenantID, tableID uuid.UUID, objectID string) error {
	key := tenantID.String() + ":" + tableID.String() + ":" + objectID
	_, err := w.db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key)
	if err != nil {
		return fmt.Errorf("lock tenant object for ingestion: %w", err)
	}
	return nil
}

func (w TenantDataWriter) readExistingBucketValues(
	ctx context.Context,
	schemaName, tableName, lookupField, objectID string,
	definitions []ingestion.LogicalBucketDefinition,
) (bool, map[string]any, error) {
	fieldNames := make([]string, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if _, ok := seen[definition.TimestampFieldName]; ok {
			continue
		}
		seen[definition.TimestampFieldName] = struct{}{}
		fieldNames = append(fieldNames, definition.TimestampFieldName)
	}
	sort.Strings(fieldNames)
	if len(fieldNames) == 0 {
		var marker int
		query := fmt.Sprintf(
			`SELECT 1 FROM %s WHERE %s = $1 FOR UPDATE`,
			sanitizeIdentifier(schemaName, tableName),
			sanitizeIdentifier(lookupField),
		)
		err := w.db.QueryRow(ctx, query, objectID).Scan(&marker)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, map[string]any{}, nil
		}
		return err == nil, map[string]any{}, err
	}
	columns := make([]string, len(fieldNames))
	values := make([]any, len(fieldNames))
	destinations := make([]any, len(fieldNames))
	for i, fieldName := range fieldNames {
		columns[i] = sanitizeIdentifier(fieldName)
		destinations[i] = &values[i]
	}
	query := fmt.Sprintf(
		`SELECT %s FROM %s WHERE %s = $1 FOR UPDATE`,
		strings.Join(columns, ", "),
		sanitizeIdentifier(schemaName, tableName),
		sanitizeIdentifier(lookupField),
	)
	if err := w.db.QueryRow(ctx, query, objectID).Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, map[string]any{}, nil
		}
		return false, nil, err
	}
	out := make(map[string]any, len(fieldNames))
	for i, fieldName := range fieldNames {
		out[fieldName] = values[i]
	}
	return true, out, nil
}

type bucketGenerationKey struct {
	definitionID uuid.UUID
	version      int
	start        time.Time
}

func (w TenantDataWriter) bumpAffectedBuckets(
	ctx context.Context,
	model ingestion.PublishedDataModel,
	table ingestion.ObjectSchema,
	definitions []ingestion.LogicalBucketDefinition,
	oldValues, newRecord map[string]any,
	existed bool,
	now time.Time,
) error {
	keys := make(map[bucketGenerationKey]struct{}, len(definitions)*2)
	for _, definition := range definitions {
		if existed {
			if oldTime, ok := timestampValue(oldValues[definition.TimestampFieldName]); ok {
				start, err := logicalDayStart(oldTime, definition.Timezone)
				if err != nil {
					return err
				}
				keys[bucketGenerationKey{definition.ID, definition.DefinitionVersion, start}] = struct{}{}
			}
		}
		newValue, provided := newRecord[definition.TimestampFieldName]
		if !provided {
			newValue = oldValues[definition.TimestampFieldName]
		}
		newTime, ok := timestampValue(newValue)
		if !ok {
			return fmt.Errorf("logical bucket timestamp %s is required", definition.TimestampFieldName)
		}
		start, err := logicalDayStart(newTime, definition.Timezone)
		if err != nil {
			return err
		}
		keys[bucketGenerationKey{definition.ID, definition.DefinitionVersion, start}] = struct{}{}
	}
	for key := range keys {
		_, err := w.db.Exec(ctx, `
			INSERT INTO core_ingestion.logical_bucket_generations (
				tenant_id, table_id, bucket_definition_id, definition_version,
				bucket_start_utc, generation, last_changed_at
			)
			VALUES ($1,$2,$3,$4,$5,1,$6)
			ON CONFLICT (
				tenant_id, table_id, bucket_definition_id, definition_version,
				bucket_start_utc
			)
			DO UPDATE SET
				generation = core_ingestion.logical_bucket_generations.generation + 1,
				last_changed_at = EXCLUDED.last_changed_at
		`, model.TenantID, table.ID, key.definitionID, key.version, key.start, now)
		if err != nil {
			return fmt.Errorf("increment logical bucket generation: %w", err)
		}
	}
	return nil
}

func timestampValue(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case *time.Time:
		if typed != nil {
			return *typed, true
		}
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func logicalDayStart(value time.Time, timezone string) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load logical bucket timezone %s: %w", timezone, err)
	}
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC(), nil
}

func tenantSchemaName(tenantID uuid.UUID) string {
	return "tenant_" + strings.ReplaceAll(tenantID.String(), "-", "")
}
