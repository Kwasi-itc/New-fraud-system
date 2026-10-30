package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/netip"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrRecordConflict = errors.New("ingestion record unique constraint conflict")

type RecordWriteResult struct {
	ID     uuid.UUID
	Action string
}
type mappedWriteField struct {
	definition ingestion.FieldSchema
	physical   WriteField
}

// RecordWriter uses one authorized owner transaction; results remain provisional
// until the owner commits data/audit/outbox/receipt together. It cannot install
// schema, authorize a request, select a newer version or commit independently.
type RecordWriter struct {
	tx                                               *sql.Tx
	database, qualified, objectColumn, updatedColumn string
	tenant, table                                    uuid.UUID
	fields                                           map[string]mappedWriteField
}

func NewRecordWriter(ctx context.Context, tx *sql.Tx, database string, tenant uuid.UUID, model ingestion.PublishedDataModel, objectType string, expected uuid.UUID) (*RecordWriter, error) {
	table, ok := model.Tables[objectType]
	version := model.PublicationVersion
	if version == uuid.Nil {
		version, _ = uuid.Parse(model.RevisionID)
	}
	if !ok || model.TenantID != tenant || !model.Writable || table.Archived || (model.RecordLookupField != "" && model.RecordLookupField != "object_id") || version != expected {
		return nil, ErrWriteUnavailable
	}
	snapshot, err := LockWriteSnapshot(ctx, tx, database, tenant, table.ID, expected, 1000)
	if err != nil {
		return nil, err
	}
	if len(table.Fields) != len(snapshot.Fields) {
		return nil, ErrWriteMapping
	}
	byID := make(map[uuid.UUID]WriteField, len(snapshot.Fields))
	for _, field := range snapshot.Fields {
		byID[field.ID] = field
	}
	schema, _ := mysqlstore.Identifier(database)
	name, err := mysqlstore.Identifier(snapshot.PhysicalName)
	if err != nil {
		return nil, err
	}
	w := &RecordWriter{tx: tx, database: database, qualified: schema + "." + name, tenant: tenant, table: table.ID, fields: make(map[string]mappedWriteField)}
	used := make(map[uuid.UUID]bool)
	for logical, field := range table.Fields {
		physical, ok := byID[field.ID]
		if !ok || used[field.ID] || field.Name != logical || physical.Type != field.DataType || physical.Archived != field.Archived {
			return nil, ErrWriteMapping
		}
		used[field.ID] = true
		// Copy only values needed by the writer; enum/model validation is upstream.
		field.EnumValues = nil
		w.fields[logical] = mappedWriteField{field, physical}
	}
	object, ok := w.fields["object_id"]
	updated, hasUpdated := w.fields["updated_at"]
	if !ok || !hasUpdated || object.definition.Archived || updated.definition.Archived || object.definition.DataType != "string" || !object.definition.IsUnique || object.definition.Nullable || object.definition.IsEnum || updated.definition.DataType != "timestamp" || updated.definition.Nullable {
		return nil, ErrWriteMapping
	}
	w.objectColumn = "`" + object.physical.Components["value"] + "`"
	w.updatedColumn = "`" + updated.physical.Components["value"] + "`"
	return w, nil
}

// WriteBatch accepts normalized, model-validated records. Lookup guards serialize
// absent keys; full-byte lookup then chooses INSERT or UPDATE. Alternate unique
// failures never redirect to a different row. An unchanged UPDATE is success.
func (w *RecordWriter) WriteBatch(ctx context.Context, records []map[string]any, mode ingestion.Mode, now time.Time, maxBytes int) ([]RecordWriteResult, error) {
	if w == nil || w.tx == nil || len(records) < 1 || len(records) > 500 || (mode != ingestion.ModeCreate && mode != ingestion.ModePatch) || maxBytes < 1 || maxBytes > 16<<20 {
		return nil, errors.New("bounded normalized record batch required")
	}
	at, err := (mysqlstore.DateTime{Time: now}).Value()
	if err != nil {
		return nil, err
	}
	type prepared struct {
		key     string
		columns []string
		values  []any
	}
	batch := make([]prepared, len(records))
	keys := make([]string, len(records))
	remaining := maxBytes
	for i, record := range records {
		if len(record) > 1000 {
			return nil, errors.New("record field count exceeds bound")
		}
		key, ok := record["object_id"].(string)
		if !ok || key == "" || len(key) > 1<<20 || !utf8.ValidString(key) {
			return nil, errors.New("normalized object_id required")
		}
		keys[i] = key
		p := prepared{key: key, columns: []string{w.updatedColumn}, values: []any{at}}
		logical := make([]string, 0, len(record))
		for field := range record {
			if field != "object_id" {
				logical = append(logical, field)
			}
		}
		sort.Strings(logical)
		for _, name := range logical {
			field, exists := w.fields[name]
			if !exists || field.definition.Archived || name == "updated_at" {
				return nil, errors.New("unknown, archived or managed record field")
			}
			parts, values, err := encodeWriteValue(field.definition, record[name])
			if err != nil {
				return nil, err
			}
			for j, part := range parts {
				p.columns = append(p.columns, "`"+field.physical.Components[part]+"`")
				p.values = append(p.values, values[j])
			}
		}
		remaining -= len(key)
		for _, value := range p.values {
			size := 16
			switch v := value.(type) {
			case string:
				size = len(v)
			case []byte:
				size = len(v)
			case nil:
				size = 0
			}
			remaining -= size
		}
		if remaining < 0 {
			return nil, errors.New("record batch byte limit exceeded")
		}
		batch[i] = p
	}
	if err := LockLookupKeys(ctx, w.tx, w.database, w.tenant, w.table, keys, 1<<20, maxBytes); err != nil {
		return nil, err
	}
	results := make([]RecordWriteResult, 0, len(batch))
	for _, p := range batch {
		keyID, found, err := FindRecordKey(ctx, w.tx, w.database, w.tenant, w.table, p.key)
		if err != nil {
			return nil, err
		}
		var id mysqlstore.UUID
		exists := false
		if found {
			err := w.tx.QueryRowContext(ctx, "SELECT id FROM "+w.qualified+" WHERE record_object_key_id=? AND CAST("+w.objectColumn+" AS BINARY)=? FOR UPDATE", mysqlstore.UUID(keyID), []byte(p.key)).Scan(&id)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			exists = err == nil
		}
		action := "updated"
		if exists {
			assignments := make([]string, len(p.columns))
			for i, column := range p.columns {
				assignments[i] = column + "=?"
			}
			args := append(append([]any(nil), p.values...), id)
			_, err = w.tx.ExecContext(ctx, "UPDATE "+w.qualified+" SET "+strings.Join(assignments, ",")+" WHERE id=?", args...)
		} else {
			action = "created"
			id = mysqlstore.UUID(uuid.New())
			columns := append([]string{"`id`", w.objectColumn}, p.columns...)
			args := append([]any{id, p.key}, p.values...)
			_, err = w.tx.ExecContext(ctx, "INSERT INTO "+w.qualified+" ("+strings.Join(columns, ",")+") VALUES ("+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+")", args...)
		}
		if err != nil {
			if mysqlstore.Classify(err) == mysqlstore.Duplicate {
				return nil, errors.Join(ErrRecordConflict, err)
			}
			return nil, err
		}
		// Trigger-derived identity must be present and match the owner lookup.
		var actual mysqlstore.UUID
		if err := w.tx.QueryRowContext(ctx, "SELECT record_object_key_id FROM "+w.qualified+" WHERE id=?", id).Scan(&actual); err != nil {
			return nil, err
		}
		resolved, ok, err := FindRecordKey(ctx, w.tx, w.database, w.tenant, w.table, p.key)
		if err != nil {
			return nil, err
		}
		if !ok || resolved != uuid.UUID(actual) {
			return nil, errors.New("record key readback differs from exact lookup")
		}
		results = append(results, RecordWriteResult{ID: uuid.UUID(id), Action: action})
	}
	return results, nil
}

func encodeWriteValue(field ingestion.FieldSchema, value any) ([]string, []any, error) {
	parts := []string{"value"}
	if field.DataType == "ip_address" {
		parts = []string{"address", "family", "prefix"}
	}
	if value == nil {
		if !field.Nullable {
			return nil, nil, errors.New("NULL for required record field")
		}
		return parts, make([]any, len(parts)), nil
	}
	bad := errors.New("record value does not match normalized field type")
	switch field.DataType {
	case "bool":
		if v, ok := value.(bool); ok {
			return parts, []any{v}, nil
		}
	case "int":
		if v, ok := value.(int64); ok && v >= math.MinInt32 && v <= math.MaxInt32 {
			return parts, []any{v}, nil
		}
	case "float":
		if v, ok := value.(float64); ok && !math.IsNaN(v) && !math.IsInf(v, 0) {
			return parts, []any{v}, nil
		}
	case "string":
		if v, ok := value.(string); ok && utf8.ValidString(v) {
			return parts, []any{v}, nil
		}
	case "timestamp":
		if v, ok := value.(time.Time); ok {
			encoded, err := (mysqlstore.DateTime{Time: v}).Value()
			return parts, []any{encoded}, err
		}
	case "ip_address":
		if v, ok := value.(string); ok {
			addr, err := netip.ParseAddr(v)
			if err != nil || addr.Zone() != "" {
				return nil, nil, bad
			}
			encoded, err := mysqlstore.EncodeInet(netip.PrefixFrom(addr, addr.BitLen()))
			return parts, []any{encoded.Address, encoded.Family, encoded.PrefixBits}, err
		}
	}
	return nil, nil, bad
}
