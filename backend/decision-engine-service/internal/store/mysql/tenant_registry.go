package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrPublishedModelUnavailable = ports.ErrPublishedModelUnavailable
var ErrAggregateCapability = ports.ErrAggregateCapability

// TenantDataReader consumes data-model-owned read contracts, never mutates owner
// metadata and never guesses a physical identifier. Each call pins its registry
// gate through the data query in one short transaction. It deliberately is not
// a global evaluation snapshot. Model acquisition is outside that transaction;
// closed/stale gates fail without falling back to HTTP or another model.
type TenantDataReader struct {
	configuration  *PlatformStore
	models         ports.TenantModelReader
	snapshot       *ports.TenantModel
	snapshotTenant string
}

var _ ports.TenantDataReader = TenantDataReader{}
var _ ports.ModelScopedReader = TenantDataReader{}

func NewTenantDataReader(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int, models ports.TenantModelReader, authorize PlatformAuthorizer) (TenantDataReader, error) {
	c, err := NewPlatformStore(db, database, tenant, maxRows, maxBytes, authorize)
	if err != nil {
		return TenantDataReader{}, err
	}
	if models == nil {
		return TenantDataReader{}, errors.New("published model reader required")
	}
	return TenantDataReader{configuration: c, models: models}, nil
}
func (r TenantDataReader) WithModel(tenant string, model ports.TenantModel) ports.TenantDataReader {
	// Deep copy the supplied snapshot: callers cannot change authority or field
	// types while a query is being prepared. The model is an immutable value here.
	copyModel := model
	copyModel.Tables = make(map[string]ports.TenantModelTable, len(model.Tables))
	for name, table := range model.Tables {
		copyTable := table
		copyTable.Fields = make(map[string]ports.TenantModelField, len(table.Fields))
		for key, field := range table.Fields {
			field.EnumValues = append([]string(nil), field.EnumValues...)
			copyTable.Fields[key] = field
		}
		copyTable.LinksToSingle = nil
		copyModel.Tables[name] = copyTable
	}
	r.snapshot, r.snapshotTenant = &copyModel, tenant
	return r
}

type registryDocument struct {
	ID                  uuid.UUID                   `json:"id"`
	Name                string                      `json:"name"`
	Archived            bool                        `json:"archived"`
	Fields              map[string]registryField    `json:"fields"`
	TextEqualityIndexes []registryTextEqualityIndex `json:"text_equality_indexes,omitempty"`
}
type registryField struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"data_type"`
	Nullable   bool      `json:"nullable"`
	Archived   bool      `json:"archived"`
	IsEnum     bool      `json:"is_enum"`
	IsUnique   bool      `json:"is_unique"`
	EnumValues []struct {
		Value string `json:"value"`
	} `json:"enum_values"`
	columns          map[string]string
	textEqualityHash string
}
type registryTable struct {
	id, version uuid.UUID
	qualified   string
	fields      map[string]registryField
	ordered     []string
	lookup      string
}

func (r TenantDataReader) read(ctx context.Context, tenant, object string, fn func(authoringRepository, registryTable) error) error {
	c := r.configuration
	if c == nil || c.tenant.String() != tenant {
		return ErrTenantScope
	}
	var model ports.TenantModel
	var err error
	if r.snapshot != nil {
		if r.snapshotTenant != tenant {
			return ErrTenantScope
		}
		model = *r.snapshot
	} else {
		model, err = r.models.GetTenantModel(ctx, tenant)
		if err != nil {
			return err
		}
	}
	table, ok := model.Tables[object]
	if !ok || table.Archived || table.Name != object {
		return ErrPublishedModelUnavailable
	}
	id, err := uuid.Parse(table.ID)
	if err != nil || id == uuid.Nil {
		return ErrPublishedModelUnavailable
	}
	version, err := uuid.Parse(table.PublishedVersion)
	if err != nil || version == uuid.Nil {
		return ErrPublishedModelUnavailable
	}
	// Registry locking is a current read: do not mix it into the read-only
	// repeatable snapshot helper. No owner DML or external calls occur here.
	err = mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := c.authorize(ctx, tx, c.tenant, false); err != nil {
			return err
		}
		repository := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		resolved, err := repository.registry(ctx, id, version, model, table)
		if err != nil {
			return err
		}
		return fn(repository, resolved)
	})
	if err != nil {
		return &ports.LocalTenantReadError{Cause: err}
	}
	return nil
}
func (r authoringRepository) registry(ctx context.Context, table, expected uuid.UUID, model ports.TenantModel, want ports.TenantModelTable) (registryTable, error) {
	var physical, state string
	var version, active mysqlstore.NullUUID
	var epoch int64
	args := []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(table)}
	if err := r.tx.QueryRowContext(ctx, "SELECT physical_name,published_version,epoch,write_state,active_operation FROM "+r.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR SHARE", args...).Scan(&physical, &version, &epoch, &state, &active); err != nil {
		return registryTable{}, errors.Join(ErrPublishedModelUnavailable, err)
	}
	if state != "open" || active.Valid || !version.Valid || uuid.UUID(version.UUID) != expected || epoch < 0 || !strings.HasPrefix(physical, "tenant_") || !strings.HasSuffix(physical, "_"+strings.ReplaceAll(table.String(), "-", "")) {
		return registryTable{}, ErrPublishedModelUnavailable
	}
	name, err := mysqlstore.Identifier(physical)
	if err != nil {
		return registryTable{}, ErrPublishedModelUnavailable
	}
	var published mysqlstore.UUID
	var document mysqlstore.JSON
	var size int64
	var logical mysqlstore.Text
	query := "SELECT version,OCTET_LENGTH(payload)+OCTET_LENGTH(table_name),CASE WHEN OCTET_LENGTH(payload)+OCTET_LENGTH(table_name)<=? THEN payload ELSE 'null' END,CASE WHEN OCTET_LENGTH(payload)+OCTET_LENGTH(table_name)<=? THEN table_name ELSE '' END FROM " + r.schema + ".data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?"
	if err := r.tx.QueryRowContext(ctx, query, r.maxBytes, r.maxBytes, args[0], args[1]).Scan(&published, &size, &document, &logical); err != nil {
		return registryTable{}, errors.Join(ErrPublishedModelUnavailable, err)
	}
	if size > int64(r.maxBytes) {
		return registryTable{}, ErrProjectionLimit
	}
	if uuid.UUID(published) != expected || string(logical) != want.Name {
		return registryTable{}, ErrPublishedModelUnavailable
	}
	var doc registryDocument
	if err := json.Unmarshal(document, &doc); err != nil {
		return registryTable{}, ErrPublishedModelUnavailable
	}
	if doc.ID != table || doc.Name != want.Name || doc.Archived || len(doc.Fields) < 1 || len(doc.Fields) > 1000 {
		return registryTable{}, ErrPublishedModelUnavailable
	}
	byID := map[uuid.UUID]string{}
	result := registryTable{id: table, version: expected, qualified: r.schema + "." + name, fields: map[string]registryField{}, lookup: model.RecordLookupField}
	for key, field := range doc.Fields {
		if key != field.Name || field.ID == uuid.Nil || byID[field.ID] != "" {
			return registryTable{}, ErrPublishedModelUnavailable
		}
		byID[field.ID] = key
		field.columns = map[string]string{}
		if !field.Archived {
			target, ok := want.Fields[key]
			if !ok || target.Archived || target.Name != key || target.Type != field.Type || target.Nullable != field.Nullable || target.IsEnum != field.IsEnum {
				return registryTable{}, ErrPublishedModelUnavailable
			}
			if field.IsEnum {
				values := make([]string, len(field.EnumValues))
				for i, v := range field.EnumValues {
					values[i] = v.Value
				}
				a, b := append([]string(nil), values...), append([]string(nil), target.EnumValues...)
				sort.Strings(a)
				sort.Strings(b)
				if !slices.Equal(a, b) {
					return registryTable{}, ErrPublishedModelUnavailable
				}
			}
		}
		result.fields[key] = field
	}
	for key, field := range want.Fields {
		if !field.Archived {
			if published, ok := doc.Fields[key]; !ok || published.Archived {
				return registryTable{}, ErrPublishedModelUnavailable
			}
		}
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT field_id,component,physical_name FROM "+r.schema+".data_model_physical_fields WHERE tenant_id=? AND table_id=? ORDER BY field_id,component LIMIT 3001", args...)
	if err != nil {
		return registryTable{}, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	count := 0
	for rows.Next() {
		count++
		if count > 3000 {
			return registryTable{}, ErrProjectionLimit
		}
		var id mysqlstore.UUID
		var component, column string
		if err := rows.Scan(&id, &component, &column); err != nil {
			return registryTable{}, err
		}
		fieldName, ok := byID[uuid.UUID(id)]
		if !ok {
			return registryTable{}, ErrPublishedModelUnavailable
		}
		field := result.fields[fieldName]
		allowed := component == "value" && field.Type != "ip_address" || (field.Type == "ip_address" && (component == "family" || component == "address" || component == "prefix"))
		if !allowed || field.columns[component] != "" || seen[column] || !strings.HasPrefix(column, "field_") || !strings.HasSuffix(column, "_"+strings.ReplaceAll(uuid.UUID(id).String(), "-", "")+"_"+component) {
			return registryTable{}, ErrPublishedModelUnavailable
		}
		quoted, err := mysqlstore.Identifier(column)
		if err != nil {
			return registryTable{}, ErrPublishedModelUnavailable
		}
		field.columns[component] = quoted
		seen[column] = true
		result.fields[fieldName] = field
	}
	if err := rows.Err(); err != nil {
		return registryTable{}, err
	}
	for key, field := range result.fields {
		wantColumns := 1
		if field.Type == "ip_address" {
			wantColumns = 3
		}
		if len(field.columns) != wantColumns {
			return registryTable{}, ErrPublishedModelUnavailable
		}
		switch field.Type {
		case "bool", "int", "float", "string", "timestamp", "ip_address":
		default:
			return registryTable{}, ErrPublishedModelUnavailable
		}
		if !field.Archived {
			result.ordered = append(result.ordered, key)
		}
	}
	sort.Strings(result.ordered)
	if err := result.bindTextEqualityIndexes(doc.TextEqualityIndexes); err != nil {
		return registryTable{}, err
	}
	lookup, ok := result.fields[result.lookup]
	if !ok || lookup.Archived || lookup.Nullable || lookup.Type != "string" || !lookup.IsUnique {
		return registryTable{}, ErrPublishedModelUnavailable
	}
	return result, nil
}
func (t registryTable) field(name string) (registryField, error) {
	f, ok := t.fields[name]
	if !ok || f.Archived {
		return registryField{}, ErrAggregateCapability
	}
	return f, nil
}

func (r TenantDataReader) GetRecord(ctx context.Context, tenant, object, id string) (ports.TenantRecord, error) {
	var result ports.TenantRecord
	err := r.read(ctx, tenant, object, func(repo authoringRepository, t registryTable) error {
		if err := repo.scope(tenant, id); err != nil {
			return err
		}
		where, args, err := repo.recordLookup(ctx, t, id)
		if err != nil {
			return err
		}
		records, err := repo.records(ctx, t, object, " WHERE "+where, args, 1)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return sql.ErrNoRows
		}
		result = records[0]
		return nil
	})
	return result, err
}
func (r TenantDataReader) ListRecords(ctx context.Context, tenant, object string, limit int) ([]ports.TenantRecord, error) {
	return r.query(ctx, tenant, object, "", "", limit)
}
func (r TenantDataReader) QueryRecords(ctx context.Context, tenant, object, field, value string, limit int) ([]ports.TenantRecord, error) {
	if field == "" {
		return nil, ErrAggregateCapability
	}
	return r.query(ctx, tenant, object, field, value, limit)
}
func (r TenantDataReader) query(ctx context.Context, tenant, object, field, value string, limit int) ([]ports.TenantRecord, error) {
	var result []ports.TenantRecord
	err := r.read(ctx, tenant, object, func(repo authoringRepository, t registryTable) error {
		if limit < 1 || limit > repo.maxRows {
			return ErrProjectionLimit
		}
		where := ""
		args := []any{}
		if field != "" {
			if err := repo.scope(tenant, value); err != nil {
				return err
			}
			f, err := t.field(field)
			if err != nil {
				return err
			}
			if field == t.lookup {
				expr, bound, err := repo.recordLookup(ctx, t, value)
				if errors.Is(err, sql.ErrNoRows) {
					result = []ports.TenantRecord{}
					return nil
				}
				if err != nil {
					return err
				}
				where = " WHERE " + expr
				args = bound
			} else {
				typed, err := f.queryText(value)
				if err != nil {
					return err
				}
				expr, values, err := f.predicate("eq", typed)
				if err != nil {
					return err
				}
				where = " WHERE " + expr
				args = append(args, values...)
			}
		}
		updated, err := t.field("updated_at")
		if err != nil || updated.Type != "timestamp" {
			return ErrPublishedModelUnavailable
		}
		lookup, _ := t.field(t.lookup)
		where += " ORDER BY " + updated.columns["value"] + " DESC," + lookup.columns["value"] + " DESC"
		result, err = repo.records(ctx, t, object, where, args, limit)
		return err
	})
	return result, err
}

func (r authoringRepository) records(ctx context.Context, t registryTable, object, where string, args []any, limit int) ([]ports.TenantRecord, error) {
	if limit < 1 || limit > r.maxRows {
		return nil, ErrProjectionLimit
	}
	expressions, lengths := []string{}, []string{}
	for _, key := range t.ordered {
		f := t.fields[key]
		components := []string{"value"}
		if f.Type == "ip_address" {
			components = []string{"family", "address", "prefix"}
		}
		for _, component := range components {
			column := f.columns[component]
			expressions = append(expressions, column)
			lengths = append(lengths, "COALESCE(OCTET_LENGTH("+column+"),0)")
		}
	}
	size := strings.Join(lengths, "+")
	projection := []string{size}
	for _, column := range expressions {
		projection = append(projection, "CASE WHEN "+size+"<="+strconv.Itoa(r.maxBytes)+" THEN "+column+" ELSE NULL END")
	}
	rows, err := r.tx.QueryContext(ctx, "SELECT "+strings.Join(projection, ",")+" FROM "+t.qualified+where+" LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.TenantRecord{}
	var used int64
	for rows.Next() {
		var bytes int64
		raw := make([]any, len(expressions))
		dest := []any{&bytes}
		for i := range raw {
			dest = append(dest, &raw[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if bytes < 0 || bytes > int64(r.maxBytes)-used {
			return nil, ErrProjectionLimit
		}
		used += bytes
		fields := map[string]any{}
		i := 0
		for _, name := range t.ordered {
			field := t.fields[name]
			n := 1
			if field.Type == "ip_address" {
				n = 3
			}
			value, err := field.decode(raw[i : i+n])
			if err != nil {
				return nil, err
			}
			fields[name] = value
			i += n
		}
		id, ok := fields[t.lookup].(string)
		if !ok {
			return nil, ErrPublishedModelUnavailable
		}
		out = append(out, ports.TenantRecord{ObjectID: id, ObjectType: object, Fields: fields})
	}
	return out, rows.Err()
}
