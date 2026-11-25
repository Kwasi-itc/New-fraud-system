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
	db          txExecutor
	rollupCache *RollupDefinitionCache
	deltaBuf    *[]ingestion.RollupDelta
}

func NewTenantDataWriter(db txExecutor) TenantDataWriter {
	return TenantDataWriter{db: db}
}

func NewTenantDataWriterWithRollups(db txExecutor, cache *RollupDefinitionCache, deltaBuf *[]ingestion.RollupDelta) TenantDataWriter {
	return TenantDataWriter{
		db:          db,
		rollupCache: cache,
		deltaBuf:    deltaBuf,
	}
}

func (w TenantDataWriter) UpsertRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType string, record map[string]any, mode ingestion.Mode, now time.Time) (string, error) {
	table, ok := model.Tables[objectType]
	if !ok {
		return "", fmt.Errorf("object type %s is not available for ingestion", objectType)
	}

	var activeDefs []ingestion.RollupDefinition
	if w.rollupCache != nil {
		var err error
		activeDefs, err = w.rollupCache.GetActiveForTable(ctx, w.db, model.TenantID.String(), table.Name)
		if err != nil {
			return "", err
		}
	}

	objectID, _ := record[model.RecordLookupField].(string)
	schemaName := schemaNameForModel(model)
	if mode == ingestion.ModePatch {
		deltas, err := w.patchRecord(ctx, model.TenantID.String(), schemaName, table.Name, model.RecordLookupField, objectID, record, now, activeDefs)
		if err == nil {
			if err := w.recordDeltas(ctx, deltas); err != nil {
				return "", err
			}
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

	watched := collectWatchedRollupFields(activeDefs)
	returningClause := "id"
	if len(watched) > 0 {
		parts := []string{"id"}
		for _, wf := range watched {
			parts = append(parts, watchedSelectExpr("", wf))
		}
		returningClause = strings.Join(parts, ", ")
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (%s)
		VALUES (%s)
		ON CONFLICT (%s) DO NOTHING
		RETURNING %s
	`,
		sanitizeIdentifier(schemaName, table.Name),
		strings.Join(insertColumns, ", "),
		strings.Join(placeholders, ", "),
		sanitizeIdentifier(model.RecordLookupField),
		returningClause,
	)

	var insertedID uuid.UUID
	scanDest := make([]any, 1+len(watched))
	scanDest[0] = &insertedID
	newSnap := make([]watchedFieldHolder, len(watched))
	for i, wf := range watched {
		scanDest[1+i] = newSnap[i].scanTarget(wf.kind)
	}

	err := w.db.QueryRow(ctx, query, values...).Scan(scanDest...)
	if err == nil {
		if len(activeDefs) > 0 {
			newVals := watchedValuesMap(watched, newSnap)
			deltas := buildInsertDeltas(model.TenantID.String(), activeDefs, newVals)
			if err := w.recordDeltas(ctx, deltas); err != nil {
				return "", err
			}
		}
		return "created", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// A concurrent insert is visible to this next READ COMMITTED statement.
	// UPDATE RETURNING prevents reporting success if the row disappeared.
	deltas, err := w.patchRecord(ctx, model.TenantID.String(), schemaName, table.Name, model.RecordLookupField, objectID, record, now, activeDefs)
	if err != nil {
		return "", err
	}
	if err := w.recordDeltas(ctx, deltas); err != nil {
		return "", err
	}
	return "updated", nil
}

func (w TenantDataWriter) recordDeltas(ctx context.Context, deltas []ingestion.RollupDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	if w.deltaBuf != nil {
		*w.deltaBuf = append(*w.deltaBuf, deltas...)
		return nil
	}
	if tx, ok := w.db.(pgx.Tx); ok {
		return insertRollupDeltasTx(ctx, tx, deltas)
	}
	return nil
}

func (w TenantDataWriter) patchRecord(
	ctx context.Context,
	tenantID, schemaName, tableName, lookupField, objectID string,
	record map[string]any,
	now time.Time,
	activeDefs []ingestion.RollupDefinition,
) ([]ingestion.RollupDelta, error) {
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
	objectIDParam := len(values)
	qualifiedTable := sanitizeIdentifier(schemaName, tableName)
	lookupIdent := sanitizeIdentifier(lookupField)

	watched := collectWatchedRollupFields(activeDefs)
	if len(watched) == 0 {
		query := fmt.Sprintf(`
			UPDATE %s
			SET %s
			WHERE %s = $%d
			RETURNING id
		`,
			qualifiedTable,
			strings.Join(assignments, ", "),
			lookupIdent,
			objectIDParam,
		)
		var id uuid.UUID
		return nil, w.db.QueryRow(ctx, query, values...).Scan(&id)
	}

	oldSelects := make([]string, 1+len(watched))
	oldSelects[0] = "id"
	retSelects := make([]string, 1+len(watched))
	retSelects[0] = "t.id"
	finalSelects := make([]string, 1+2*len(watched))
	finalSelects[0] = "updated.id"
	for i, wf := range watched {
		alias := fmt.Sprintf("f_%d", i)
		oldSelects[1+i] = watchedSelectExpr("", wf) + " AS " + alias
		retSelects[1+i] = watchedSelectExpr("t", wf) + " AS " + alias
		finalSelects[1+i] = "old_row." + alias
		finalSelects[1+len(watched)+i] = "updated." + alias
	}

	query := fmt.Sprintf(`
		WITH old_row AS (
			SELECT %s
			FROM %s
			WHERE %s = $%d
			FOR UPDATE
		), updated AS (
			UPDATE %s AS t
			SET %s
			FROM old_row
			WHERE t.id = old_row.id
			RETURNING %s
		)
		SELECT %s
		FROM updated
		JOIN old_row ON old_row.id = updated.id
	`,
		strings.Join(oldSelects, ", "),
		qualifiedTable,
		lookupIdent,
		objectIDParam,
		qualifiedTable,
		strings.Join(assignments, ", "),
		strings.Join(retSelects, ", "),
		strings.Join(finalSelects, ", "),
	)

	var id uuid.UUID
	oldSnap := make([]watchedFieldHolder, len(watched))
	newSnap := make([]watchedFieldHolder, len(watched))
	scanDest := make([]any, 1+2*len(watched))
	scanDest[0] = &id
	for i, wf := range watched {
		scanDest[1+i] = oldSnap[i].scanTarget(wf.kind)
		scanDest[1+len(watched)+i] = newSnap[i].scanTarget(wf.kind)
	}
	if err := w.db.QueryRow(ctx, query, values...).Scan(scanDest...); err != nil {
		return nil, err
	}

	oldVals := watchedValuesMap(watched, oldSnap)
	newVals := watchedValuesMap(watched, newSnap)
	return buildUpdateDeltas(tenantID, activeDefs, oldVals, newVals), nil
}

type watchedFieldKind int

const (
	watchedFieldText watchedFieldKind = iota
	watchedFieldTime
	watchedFieldNumericText
)

type watchedField struct {
	name string
	kind watchedFieldKind
}

type watchedFieldHolder struct {
	text *string
	ts   *time.Time
}

func (h *watchedFieldHolder) scanTarget(kind watchedFieldKind) any {
	if kind == watchedFieldTime {
		return &h.ts
	}
	return &h.text
}

func collectWatchedRollupFields(defs []ingestion.RollupDefinition) []watchedField {
	if len(defs) == 0 {
		return nil
	}
	kinds := map[string]watchedFieldKind{}
	for _, def := range defs {
		if _, ok := kinds[def.GroupField]; !ok {
			kinds[def.GroupField] = watchedFieldText
		}
		kinds[def.TimeField] = watchedFieldTime
		if def.ValueType == ingestion.RollupValueTypeInt || def.ValueType == ingestion.RollupValueTypeFloat {
			kinds[def.ValueField] = watchedFieldNumericText
		} else if _, ok := kinds[def.ValueField]; !ok {
			kinds[def.ValueField] = watchedFieldText
		}
	}
	names := make([]string, 0, len(kinds))
	for name := range kinds {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]watchedField, len(names))
	for i, name := range names {
		out[i] = watchedField{name: name, kind: kinds[name]}
	}
	return out
}

func watchedSelectExpr(prefix string, wf watchedField) string {
	col := sanitizeIdentifier(wf.name)
	if prefix != "" {
		col = prefix + "." + col
	}
	switch wf.kind {
	case watchedFieldTime:
		return col + "::timestamptz"
	case watchedFieldNumericText:
		return col + "::numeric::text"
	default:
		return col + "::text"
	}
}

func watchedValuesMap(fields []watchedField, holders []watchedFieldHolder) map[string]watchedFieldHolder {
	out := make(map[string]watchedFieldHolder, len(fields))
	for i, wf := range fields {
		out[wf.name] = holders[i]
	}
	return out
}

func buildInsertDeltas(tenantID string, defs []ingestion.RollupDefinition, newVals map[string]watchedFieldHolder) []ingestion.RollupDelta {
	var deltas []ingestion.RollupDelta
	for _, def := range defs {
		if d, ok := buildSignedTupleDelta(tenantID, def, newVals, 1); ok {
			deltas = append(deltas, d)
		}
	}
	return deltas
}

func buildUpdateDeltas(tenantID string, defs []ingestion.RollupDefinition, oldVals, newVals map[string]watchedFieldHolder) []ingestion.RollupDelta {
	var deltas []ingestion.RollupDelta
	for _, def := range defs {
		if watchedTupleEqual(def, oldVals, newVals) {
			continue
		}
		if dOld, ok := buildSignedTupleDelta(tenantID, def, oldVals, -1); ok {
			deltas = append(deltas, dOld)
		}
		if dNew, ok := buildSignedTupleDelta(tenantID, def, newVals, 1); ok {
			deltas = append(deltas, dNew)
		}
	}
	return deltas
}

func watchedTupleEqual(def ingestion.RollupDefinition, a, b map[string]watchedFieldHolder) bool {
	return holderTextEqual(a[def.GroupField].text, b[def.GroupField].text) &&
		holderTimeEqual(a[def.TimeField].ts, b[def.TimeField].ts) &&
		holderTextEqual(a[def.ValueField].text, b[def.ValueField].text)
}

func holderTextEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func holderTimeEqual(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.UTC().Equal(b.UTC())
}

func buildSignedTupleDelta(tenantID string, def ingestion.RollupDefinition, vals map[string]watchedFieldHolder, sign int64) (ingestion.RollupDelta, bool) {
	groupHolder := vals[def.GroupField]
	timeHolder := vals[def.TimeField]
	if groupHolder.text == nil || timeHolder.ts == nil {
		return ingestion.RollupDelta{}, false
	}
	d := ingestion.RollupDelta{
		TenantID:     tenantID,
		DefinitionID: def.ID,
		GroupValue:   *groupHolder.text,
		EventTime:    timeHolder.ts.UTC(),
		DRows:        sign,
		DValueCount:  0,
		DValueSum:    "0",
	}
	valHolder := vals[def.ValueField]
	if valHolder.text != nil {
		d.DValueCount = sign
		if def.ValueType == ingestion.RollupValueTypeInt || def.ValueType == ingestion.RollupValueTypeFloat {
			raw := strings.TrimSpace(*valHolder.text)
			if raw != "" {
				if sign < 0 {
					if strings.HasPrefix(raw, "-") {
						d.DValueSum = strings.TrimPrefix(raw, "-")
					} else {
						d.DValueSum = "-" + raw
					}
				} else {
					d.DValueSum = raw
				}
			}
		}
	}
	return d, true
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
