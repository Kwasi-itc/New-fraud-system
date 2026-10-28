package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// DisplayStore owns consistent reads and short READ COMMITTED mutations. The
// caller must authorize the tenant. Every schema/metadata writer must use the
// table locking and dependency maintenance protocol before runtime activation.
type DisplayStore struct {
	db                *sql.DB
	database, schema  string
	tenant            uuid.UUID
	maxRows, maxBytes int
}

func NewDisplayStore(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int) (*DisplayStore, error) {
	r, err := NewDataModelReadRepository(db, database, tenant, maxRows, maxBytes)
	if err != nil {
		return nil, err
	}
	return &DisplayStore{db, database, r.schema, tenant, maxRows, maxBytes}, nil
}

func (s *DisplayStore) repository(tx *sql.Tx) metadataRepository {
	return metadataRepository{tx, s.schema, s.tenant, s.maxRows, s.maxBytes}
}

func (s *DisplayStore) Get(ctx context.Context, table uuid.UUID) (display.Settings, error) {
	var result display.Settings
	err := mysqlstore.Snapshot(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		result, _, err = loadDisplay(ctx, s.repository(tx), table)
		return err
	})
	if err != nil {
		return display.Settings{}, err
	}
	return result, nil
}

type displayLink struct {
	value               display.Link
	parent, parentField uuid.UUID
}

func displayList[T any](ctx context.Context, r *metadataRepository, query string, args []any, scan func(metadataScanner) (T, int64, error)) ([]T, error) {
	var used int64
	values, err := metadataList(ctx, *r, query, args, func(row metadataScanner) (T, int64, error) { v, n, err := scan(row); used += n; return v, n, err })
	if err != nil {
		return nil, err
	}
	r.maxRows -= len(values)
	r.maxBytes -= int(used)
	return values, nil
}

func loadDisplay(ctx context.Context, r metadataRepository, table uuid.UUID) (display.Settings, map[uuid.UUID]displayLink, error) {
	empty := display.Settings{}
	s := display.Settings{TableID: table, TenantID: r.tenant, Fields: []display.Field{}, Links: []display.Link{}, Displayed: []uuid.UUID{}, Order: []uuid.UUID{}, Display: map[string]display.FieldDisplay{}, DisplayColumns: []display.DisplayColumn{}}
	var size int64
	var rawDisplay, rawColumns string
	err := r.tx.QueryRowContext(ctx, "SELECT t.display_version,"+r.projection("t.name", "t.caption_field", "COALESCE(CAST(o.field_display AS CHAR),'{}')", "COALESCE(CAST(o.display_columns AS CHAR),'[]')")+" FROM "+r.schema+".data_model_tables t JOIN "+r.schema+".data_model_tenants n ON n.id=t.tenant_id LEFT JOIN "+r.schema+".data_model_table_options o ON o.tenant_id=t.tenant_id AND o.table_id=t.id WHERE t.tenant_id=? AND t.id=? AND NOT t.archived AND n.status='active'", mysqlstore.UUID(r.tenant), mysqlstore.UUID(table)).Scan(&s.Version, &size, &s.Name, &s.Caption, &rawDisplay, &rawColumns)
	if err != nil {
		return empty, nil, err
	}
	if size > int64(r.maxBytes) {
		return empty, nil, ErrSnapshotLimit
	}
	r.maxBytes -= int(size)
	r.maxRows--
	if err := json.Unmarshal([]byte(rawDisplay), &s.Display); err != nil {
		return empty, nil, err
	}
	if err := json.Unmarshal([]byte(rawColumns), &s.DisplayColumns); err != nil {
		return empty, nil, err
	}
	if len(s.DisplayColumns) > display.MaxColumns {
		return empty, nil, errors.New("too many persisted display columns")
	}
	r.maxRows -= len(s.Display) + len(s.DisplayColumns)
	if r.maxRows < 0 {
		return empty, nil, ErrSnapshotLimit
	}
	options, err := (TableOptionsRepository{r}).GetByTableID(ctx, table)
	if err != nil {
		return empty, nil, err
	}
	if options != nil {
		s.Displayed, s.Order = options.DisplayedFields, options.FieldOrder
		r.maxRows -= max(1, len(s.Displayed)+len(s.Order))
	}
	links, err := displayList(ctx, &r, "SELECT l.id,l.child_field_id,l.parent_table_id,l.parent_field_id,"+r.projection("l.name", "p.name", "pf.name", "p.caption_field")+" FROM "+r.schema+".data_model_links l JOIN "+r.schema+".data_model_tables p ON p.tenant_id=l.tenant_id AND p.id=l.parent_table_id AND NOT p.archived JOIN "+r.schema+".data_model_fields pf ON pf.tenant_id=l.tenant_id AND pf.id=l.parent_field_id AND pf.table_id=p.id AND pf.is_unique AND NOT pf.archived JOIN "+r.schema+".data_model_fields cf ON cf.tenant_id=l.tenant_id AND cf.id=l.child_field_id AND cf.table_id=l.child_table_id AND NOT cf.archived AND cf.data_type=pf.data_type WHERE l.tenant_id=? AND l.child_table_id=? ORDER BY l.id", []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(table)}, func(row metadataScanner) (displayLink, int64, error) {
		v := displayLink{value: display.Link{TargetFields: []display.Field{}}}
		var n int64
		err := row.Scan((*mysqlstore.UUID)(&v.value.ID), (*mysqlstore.UUID)(&v.value.FieldID), (*mysqlstore.UUID)(&v.parent), (*mysqlstore.UUID)(&v.parentField), &n, &v.value.Name, &v.value.Table, &v.value.Field, &v.value.Caption)
		return v, n, err
	})
	if err != nil {
		return empty, nil, err
	}
	byLink := make(map[uuid.UUID]displayLink, len(links))
	tables := map[uuid.UUID]bool{table: true}
	for _, l := range links {
		tables[l.parent] = true
	}
	ids := sortedDisplayIDs(tables)
	// Each table is selected once even if multiple links share its parent. The
	// ID list is bounded by the shared row budget; queries use batches of 1000.
	byTable := make(map[uuid.UUID][]display.Field, len(ids))
	byField := make(map[uuid.UUID]*display.Field)
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{mysqlstore.UUID(r.tenant)}
		for _, id := range ids[start:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		type fieldRow struct {
			table uuid.UUID
			field display.Field
		}
		fields, err := displayList(ctx, &r, "SELECT table_id,id,"+r.projection("name", "alias", "data_type")+" FROM "+r.schema+".data_model_fields WHERE tenant_id=? AND table_id IN ("+placeholders(end-start)+") AND NOT archived ORDER BY table_id,name_key_id,id", args, func(row metadataScanner) (fieldRow, int64, error) {
			v := fieldRow{field: display.Field{Enum: map[string]string{}}}
			var n int64
			err := row.Scan((*mysqlstore.UUID)(&v.table), (*mysqlstore.UUID)(&v.field.ID), &n, &v.field.Name, &v.field.Alias, &v.field.Type)
			return v, n, err
		})
		if err != nil {
			return empty, nil, err
		}
		for _, f := range fields {
			byTable[f.table] = append(byTable[f.table], f.field)
		}
	}
	for id, fields := range byTable {
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		byTable[id] = fields
		for i := range fields {
			byField[fields[i].ID] = &fields[i]
		}
	}
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{mysqlstore.UUID(r.tenant)}
		for _, id := range ids[start:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		_, err := displayList(ctx, &r, "SELECT e.field_id,"+r.projection("e.value", "e.label")+" FROM "+r.schema+".data_model_fields f JOIN "+r.schema+".data_model_field_enum_values e ON e.tenant_id=f.tenant_id AND e.field_id=f.id WHERE f.tenant_id=? AND f.table_id IN ("+placeholders(end-start)+") AND f.is_enum AND NOT f.archived ORDER BY e.field_id,e.sort_order,e.id", args, func(row metadataScanner) (struct{}, int64, error) {
			var id mysqlstore.UUID
			var n int64
			var value, label string
			err := row.Scan(&id, &n, &value, &label)
			if err == nil && n <= int64(r.maxBytes) {
				f := byField[uuid.UUID(id)]
				if f == nil {
					return struct{}{}, n, errors.New("display enum field missing")
				}
				f.Enum[value] = label
			}
			return struct{}{}, n, err
		})
		if err != nil {
			return empty, nil, err
		}
	}
	if fields, ok := byTable[table]; ok {
		s.Fields = fields
	}
	for _, l := range links {
		if fields, ok := byTable[l.parent]; ok {
			l.value.TargetFields = fields
		}
		byLink[l.value.ID] = l
		s.Links = append(s.Links, l.value)
	}
	return s, byLink, nil
}

func sortedDisplayIDs(set map[uuid.UUID]bool) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids
}

func placeholders(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
