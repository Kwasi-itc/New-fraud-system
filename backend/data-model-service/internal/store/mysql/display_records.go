package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type displayPhysical struct {
	name    string
	columns map[uuid.UUID]map[string]string
}
type displayGroup struct {
	settings display.Settings
	links    map[uuid.UUID]displayLink
	ids      []string
}

// ReadRecords reads one bounded batch per object type, with indexed relationship
// joins. Shared table/registry locks pin settings and DDL for this short read.
// No table scans to discover names, per-record queries or dictionary writes.
func (s *DisplayStore) ReadRecords(ctx context.Context, objects []display.Object) (display.Batch, error) {
	result := display.Batch{Columns: map[string][]display.Column{}, Records: []display.Record{}}
	if err := display.ValidateObjects(objects); err != nil {
		return result, err
	}
	groups := map[string][]string{}
	for _, o := range objects {
		groups[o.Type] = append(groups[o.Type], o.ID)
	}
	types := make([]string, 0, len(groups))
	for typ := range groups {
		types = append(types, typ)
	}
	sort.Strings(types)
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM "+s.schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(s.tenant)).Scan(&status); err != nil {
			return err
		}
		if status != "active" {
			return ErrModelUnavailable
		}
		metadataBytes, metadataRows := s.maxBytes, s.maxRows
		discovered := map[string]displayGroup{}
		tables := map[uuid.UUID]bool{}
		for _, typ := range types {
			var id mysqlstore.UUID
			err := tx.QueryRowContext(ctx, "SELECT t.id FROM "+s.schema+".data_model_ingestion_contracts c JOIN "+s.schema+".data_model_tables t ON t.tenant_id=c.tenant_id AND t.id=c.table_id WHERE c.tenant_id=? AND c.table_name_key_id="+s.schema+".data_model_find_ingestion_contract_key_v1(?,?) AND NOT t.archived", mysqlstore.UUID(s.tenant), mysqlstore.UUID(s.tenant), typ).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			settings, links, err := loadDisplay(ctx, s.repository(tx), uuid.UUID(id))
			if err != nil {
				return err
			}
			raw, e := json.Marshal(settings)
			if e != nil {
				return e
			}
			metadataBytes -= len(raw)
			metadataRows -= len(settings.Fields) + len(settings.Links) + 1
			if metadataBytes < 0 || metadataRows < 0 {
				return ErrSnapshotLimit
			}
			tables[uuid.UUID(id)] = true
			for _, l := range links {
				tables[l.parent] = true
			}
			discovered[typ] = displayGroup{settings, links, groups[typ]}
		}
		if len(tables) > s.maxRows {
			return ErrSnapshotLimit
		}
		ids := sortedDisplayIDs(tables)
		physical := map[uuid.UUID]displayPhysical{}
		// Match the display writer's UUID lock ordering. Discovery is reloaded under
		// these locks; a newly introduced parent causes a retry instead of lock inversion.
		for _, id := range ids {
			var got mysqlstore.UUID
			if err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR SHARE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)).Scan(&got); err != nil {
				return err
			}
		}
		registry, err := NewPhysicalRegistry(tx, s.database, s.tenant, s.maxRows*3)
		if err != nil {
			return err
		}
		mappingBudget := s.maxRows * 3
		for _, id := range ids {
			var version mysqlstore.UUID
			if err := tx.QueryRowContext(ctx, "SELECT published_version FROM "+s.schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)).Scan(&version); err != nil {
				return err
			}
			table, err := registry.LockPublished(ctx, id, uuid.UUID(version))
			if err != nil {
				return err
			}
			mappings, err := registry.Fields(ctx, id)
			if err != nil {
				return err
			}
			mappingBudget -= len(mappings)
			if mappingBudget < 0 {
				return ErrSnapshotLimit
			}
			name, err := mysqlstore.Identifier(table.Name)
			if err != nil {
				return err
			}
			p := displayPhysical{name: s.schema + "." + name, columns: map[uuid.UUID]map[string]string{}}
			for _, m := range mappings {
				q, e := mysqlstore.Identifier(m.Name)
				if e != nil {
					return e
				}
				if p.columns[m.FieldID] == nil {
					p.columns[m.FieldID] = map[string]string{}
				}
				p.columns[m.FieldID][m.Component] = q
			}
			physical[id] = p
		}
		found := map[display.Object]display.Record{}
		remaining := s.maxBytes
		for _, typ := range types {
			group, ok := discovered[typ]
			if !ok {
				continue
			}
			settings, links, e := loadDisplay(ctx, s.repository(tx), group.settings.TableID)
			if e != nil {
				return e
			}
			for _, l := range links {
				if !tables[l.parent] {
					return ErrPhysicalConflict
				}
			}
			projection, e := display.ResolveProjection(settings)
			if e != nil {
				return e
			}
			result.Columns[typ] = []display.Column{}
			for _, p := range projection {
				result.Columns[typ] = append(result.Columns[typ], p.Column)
			}
			query, args, e := s.displayRecordQuery(settings, links, projection, group.ids, physical, remaining)
			if e != nil {
				return e
			}
			rows, e := tx.QueryContext(ctx, query, args...)
			if e != nil {
				return e
			}
			count := 0
			for rows.Next() {
				count++
				if count > len(group.ids) {
					e = ErrPhysicalConflict
					break
				}
				var size int64
				values := make([]any, 2+len(projection))
				dest := []any{&size}
				for i := range values {
					dest = append(dest, &values[i])
				}
				if e = rows.Scan(dest...); e != nil {
					break
				}
				if size > int64(remaining) {
					e = ErrSnapshotLimit
					break
				}
				remaining -= int(size)
				id := displayText(values[0])
				object := display.Object{Type: typ, ID: id}
				if _, exists := found[object]; exists {
					e = ErrPhysicalConflict
					break
				}
				record := display.Record{Object: object, Found: true, Caption: displayText(values[1]), Values: map[string]any{}}
				for i, p := range projection {
					v := values[i+2]
					if p.Link == nil {
						v, e = displayScalar(v, p.Type)
						if e != nil {
							break
						}
					} else if v != nil {
						v = displayText(v)
					}
					if label, ok := p.Enum[displayText(v)]; ok && v != nil {
						v = label
					}
					record.Values[p.ID] = v
				}
				if e != nil {
					break
				}
				found[object] = record
			}
			e = errors.Join(e, rows.Err(), rows.Close())
			if e != nil {
				return e
			}
		}
		for _, o := range objects {
			r, ok := found[o]
			if !ok {
				r = display.Record{Object: o, Caption: o.ID, Values: map[string]any{}}
			}
			result.Records = append(result.Records, r)
		}
		raw, e := json.Marshal(result)
		if e != nil {
			return e
		}
		if len(raw) > s.maxBytes {
			return ErrSnapshotLimit
		}
		return nil
	})
	if err != nil {
		return display.Batch{}, err
	}
	return result, nil
}

func displayText(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(v)
}
func displayScalar(v any, typ string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch typ {
	case "bool":
		n, e := strconv.ParseInt(displayText(v), 10, 64)
		return n != 0, e
	case "int":
		return strconv.ParseInt(displayText(v), 10, 64)
	case "float":
		return strconv.ParseFloat(displayText(v), 64)
	case "timestamp":
		if t, ok := v.(time.Time); ok {
			return t.UTC(), nil
		}
		t, e := time.ParseInLocation("2006-01-02 15:04:05.999999", displayText(v), time.UTC)
		return t, e
	default:
		return displayText(v), nil
	}
}

func (p displayPhysical) expression(alias string, f display.Field) (string, error) {
	c := p.columns[f.ID]
	if f.Type == "ip_address" {
		if c["address"] == "" || c["prefix"] == "" || c["family"] == "" {
			return "", ErrPhysicalConflict
		}
		a := alias + "."
		return "CONCAT(INET6_NTOA(" + a + c["address"] + "),CASE WHEN (" + a + c["family"] + "=4 AND " + a + c["prefix"] + "=32) OR (" + a + c["family"] + "=6 AND " + a + c["prefix"] + "=128) THEN '' ELSE CONCAT('/'," + a + c["prefix"] + ") END)", nil
	}
	if c["value"] == "" {
		return "", ErrPhysicalConflict
	}
	return alias + "." + c["value"], nil
}
func uuidLiteral(id uuid.UUID) string {
	return "UNHEX('" + strings.ReplaceAll(id.String(), "-", "") + "')"
}

func (s *DisplayStore) displayRecordQuery(settings display.Settings, links map[uuid.UUID]displayLink, projection []display.Projection, ids []string, physical map[uuid.UUID]displayPhysical, budget int) (string, []any, error) {
	root := physical[settings.TableID]
	fields := map[string]display.Field{}
	for _, f := range settings.Fields {
		fields[f.Name] = f
	}
	object, err := root.expression("r", fields["object_id"])
	if err != nil {
		return "", nil, err
	}
	caption := object
	if f, ok := fields[settings.Caption]; ok && f.Type == "string" {
		expr, e := root.expression("r", f)
		if e != nil {
			return "", nil, e
		}
		caption = "COALESCE(NULLIF(" + expr + ",'')," + object + ")"
	}
	expressions := []string{object, caption}
	joins := []string{}
	aliases := map[uuid.UUID]string{}
	for _, col := range projection {
		expr, e := root.expression("r", col.Field)
		if e != nil {
			return "", nil, e
		}
		if col.Link != nil {
			link, ok := links[col.Link.ID]
			if !ok {
				return "", nil, ErrPhysicalConflict
			}
			parent, ok := physical[link.parent]
			if !ok {
				return "", nil, ErrPhysicalConflict
			}
			alias, exists := aliases[col.Link.ID]
			if !exists {
				alias = fmt.Sprintf("p%d", len(aliases))
				aliases[col.Link.ID] = alias
				var target display.Field
				for _, f := range col.Link.TargetFields {
					if f.ID == link.parentField {
						target = f
						break
					}
				}
				if target.ID == uuid.Nil {
					return "", nil, ErrPhysicalConflict
				}
				right, e := parent.expression(alias, target)
				if e != nil {
					return "", nil, e
				}
				on := right + "=" + expr
				if target.Type == "string" {
					key := "record_text_key_" + strings.ReplaceAll(target.ID.String(), "-", "")
					lookup := "(SELECT id FROM " + s.schema + ".data_model_exact_keys WHERE key_domain=_binary'record_field_value' AND scope_bytes=CONCAT(" + uuidLiteral(s.tenant) + "," + uuidLiteral(target.ID) + ") AND bucket_hash=" + s.schema + ".data_model_exact_key_digest(CAST(" + expr + " AS BINARY)) AND raw_value=CAST(" + expr + " AS BINARY))"
					if target.Name == "object_id" {
						key = "record_object_key_id"
						lookup = "CASE WHEN OCTET_LENGTH(" + expr + ") BETWEEN 1 AND 1048576 THEN " + s.schema + ".data_model_find_record_object_key_v1(" + uuidLiteral(s.tenant) + "," + uuidLiteral(link.parent) + ",CAST(" + expr + " AS BINARY)) ELSE NULL END"
					}
					on = alias + ".`" + key + "`=" + lookup
				} else if target.Type == "ip_address" {
					parts := []string{}
					for _, component := range []string{"address", "family", "prefix"} {
						parts = append(parts, alias+"."+parent.columns[target.ID][component]+"=r."+root.columns[col.Field.ID][component])
					}
					on = strings.Join(parts, " AND ")
				}
				joins = append(joins, "LEFT JOIN "+parent.name+" "+alias+" ON "+on+" AND "+alias+".valid_until IS NULL")
			}
			related, e := parent.expression(alias, *col.Target)
			if e != nil {
				return "", nil, e
			}
			expr = "COALESCE(NULLIF(CAST(" + related + " AS CHAR),''),CAST(" + expr + " AS CHAR))"
		}
		expressions = append(expressions, expr)
	}
	sizes := []string{}
	for _, expr := range expressions {
		sizes = append(sizes, "COALESCE(OCTET_LENGTH(CAST("+expr+" AS BINARY)),0)")
	}
	size := strings.Join(sizes, "+")
	selected := []string{"(" + size + ")"}
	for _, expr := range expressions {
		selected = append(selected, "CASE WHEN ("+size+")<="+strconv.Itoa(budget)+" THEN "+expr+" END")
	}
	requests := []string{}
	args := []any{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		requests = append(requests, "SELECT "+s.schema+".data_model_find_record_object_key_v1(?,?,?) AS key_id")
		args = append(args, mysqlstore.UUID(s.tenant), mysqlstore.UUID(settings.TableID), id)
	}
	query := "SELECT " + strings.Join(selected, ",") + " FROM (" + strings.Join(requests, " UNION ALL ") + " LIMIT 100) requested STRAIGHT_JOIN " + root.name + " r FORCE INDEX (`record_unique_" + strings.ReplaceAll(fields["object_id"].ID.String(), "-", "") + "`) ON r.record_object_key_id=requested.key_id " + strings.Join(joins, " ") + " WHERE r.valid_until IS NULL LIMIT " + strconv.Itoa(len(seen)+1)
	return query, args, nil
}
