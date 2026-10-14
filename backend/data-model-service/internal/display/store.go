package display

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
	"strings"
	"time"
)

type Store struct {
	DB           *pgxpool.Pool
	Transactions ports.TransactionManager
	SetCaption   func(context.Context, ports.MutationStore, uuid.UUID, string) error
}
type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func load(ctx context.Context, db queryer, tableID uuid.UUID, projectionOnly ...bool) (Settings, error) {
	s := Settings{Fields: []Field{}, Links: []Link{}}
	err := db.QueryRow(ctx, `SELECT t.id,t.tenant_id,t.name,n.schema_name,t.caption_field,t.display_version,
 coalesce(o.displayed_fields,'{}'::uuid[]),coalesce(o.field_order,'{}'::uuid[]),coalesce(o.field_display,'{}'::jsonb)
 FROM core.model_tables t JOIN core.tenants n ON n.id=t.tenant_id
 LEFT JOIN core.table_options o ON o.table_id=t.id AND o.tenant_id=t.tenant_id
 WHERE t.id=$1 AND NOT t.archived AND n.status='active'`, tableID).Scan(&s.TableID, &s.TenantID, &s.Name, &s.Schema, &s.Caption, &s.Version, &s.Displayed, &s.Order, &s.Display)
	if err != nil {
		return s, err
	}
	projection := len(projectionOnly) > 0 && projectionOnly[0]
	fieldFilter := ""
	args := []any{s.TableID, s.TenantID}
	if projection {
		fieldFilter = " AND (f.name=$3 OR f.id=ANY($4::uuid[]))"
		args = append(args, s.Caption, s.Displayed)
	}
	rows, err := db.Query(ctx, `SELECT f.id,f.name,f.data_type,coalesce((SELECT jsonb_object_agg(e.value,e.label) FROM core.field_enum_values e WHERE f.is_enum AND e.field_id=f.id AND e.tenant_id=f.tenant_id),'{}'::jsonb)
 FROM core.model_fields f WHERE f.table_id=$1 AND f.tenant_id=$2 AND NOT f.archived`+fieldFilter+` ORDER BY f.name`, args...)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var f Field
		if err := rows.Scan(&f.ID, &f.Name, &f.Type, &f.Enum); err != nil {
			return s, err
		}
		s.Fields = append(s.Fields, f)
	}
	if err = rows.Err(); err != nil {
		return s, err
	}
	rows.Close()
	linkFilter := ""
	args = []any{s.TableID, s.TenantID}
	if projection {
		ids := []uuid.UUID{}
		for _, id := range s.Displayed {
			if choice := s.Display[id.String()]; choice.LinkID != nil {
				ids = append(ids, *choice.LinkID)
			}
		}
		linkFilter = " AND l.id=ANY($3::uuid[])"
		args = append(args, ids)
	}
	rows, err = db.Query(ctx, `SELECT l.id,l.child_field_id,l.name,p.name,pf.name,p.caption_field
 FROM core.model_links l JOIN core.model_tables p ON p.id=l.parent_table_id AND p.tenant_id=l.tenant_id AND NOT p.archived
 JOIN core.model_fields pf ON pf.id=l.parent_field_id AND pf.table_id=p.id AND pf.tenant_id=l.tenant_id AND pf.is_unique AND NOT pf.archived
 JOIN core.model_fields cf ON cf.id=l.child_field_id AND cf.table_id=l.child_table_id AND cf.tenant_id=l.tenant_id AND NOT cf.archived AND cf.data_type=pf.data_type
 JOIN core.model_fields cap ON cap.table_id=p.id AND cap.name=p.caption_field AND cap.tenant_id=l.tenant_id AND cap.data_type='string' AND NOT cap.archived
 WHERE l.child_table_id=$1 AND l.tenant_id=$2`+linkFilter+` ORDER BY l.id`, args...)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var l Link
		if err = rows.Scan(&l.ID, &l.FieldID, &l.Name, &l.Table, &l.Field, &l.Caption); err != nil {
			return s, err
		}
		s.Links = append(s.Links, l)
	}
	return s, rows.Err()
}

func (s Store) Get(ctx context.Context, tableID uuid.UUID) (Settings, error) {
	return load(ctx, s.DB, tableID)
}
func (s Store) Save(ctx context.Context, tableID uuid.UUID, in SaveRequest) (Settings, error) {
	var result Settings
	err := s.Transactions.Run(ctx, func(ms ports.MutationStore) error {
		tx := ms.RawTx()
		// Only configuration of this table is serialized, never record reads or ingestion.
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM core.model_tables WHERE id=$1 FOR UPDATE`, tableID).Scan(&locked); err != nil {
			return err
		}
		current, err := load(ctx, tx, tableID)
		if err != nil {
			return err
		}
		if err = validate(current, in); err != nil {
			return err
		}
		if err = s.SetCaption(ctx, ms, tableID, in.Caption); err != nil {
			return err
		}
		if in.Display == nil {
			in.Display = map[string]FieldDisplay{}
		}
		if in.Displayed == nil {
			in.Displayed = []uuid.UUID{}
		}
		if in.Order == nil {
			in.Order = []uuid.UUID{}
		}
		_, err = tx.Exec(ctx, `INSERT INTO core.table_options(id,tenant_id,table_id,displayed_fields,field_order,field_display,updated_at)
  VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()) ON CONFLICT(table_id) DO UPDATE SET displayed_fields=EXCLUDED.displayed_fields,
  field_order=EXCLUDED.field_order,field_display=EXCLUDED.field_display,updated_at=EXCLUDED.updated_at`, uuid.New(), current.TenantID, tableID, in.Displayed, in.Order, in.Display)
		if err != nil {
			return err
		}
		details, err := json.Marshal(in)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO core.schema_change_log(id,tenant_id,operation,resource_type,resource_id,status,details,created_at)
  VALUES($1,$2,'update_display_settings','table',$3,'applied',$4,clock_timestamp())`, uuid.New(), current.TenantID, tableID, details); err != nil {
			return err
		}
		result, err = load(ctx, tx, tableID)
		return err
	})
	return result, err
}

func orderedFields(s Settings) []Field {
	byID := map[uuid.UUID]Field{}
	for _, f := range s.Fields {
		byID[f.ID] = f
	}
	selected := map[uuid.UUID]bool{}
	for _, id := range s.Displayed {
		selected[id] = true
	}
	out := []Field{}
	seen := map[uuid.UUID]bool{}
	for _, id := range append(append([]uuid.UUID{}, s.Order...), s.Displayed...) {
		f, ok := byID[id]
		if ok && selected[id] && !seen[id] && len(out) < MaxColumns {
			out = append(out, f)
			seen[id] = true
		}
	}
	return out
}
func quote(name string) string { return pgx.Identifier{name}.Sanitize() }

// recordQuery reads only the configured projection of ingestion-owned current rows.
// The current object_id and declared unique parent keys are indexed by their owners.
func recordQuery(s Settings, fields []Field) string {
	caption := "r.object_id"
	for _, f := range s.Fields {
		if f.Name == s.Caption && f.Type == "string" {
			caption = "coalesce(nullif(r." + quote(f.Name) + ",''),r.object_id)"
		}
	}
	projection := []string{"r.object_id", caption}
	joins := []string{}
	links := map[uuid.UUID]Link{}
	for _, l := range s.Links {
		links[l.ID] = l
	}
	for i, f := range fields {
		expr := "r." + quote(f.Name)
		if choice := s.Display[f.ID.String()]; choice.LinkID != nil {
			if link, ok := links[*choice.LinkID]; ok && link.FieldID == f.ID {
				alias := fmt.Sprintf("p%d", i)
				joins = append(joins, "LEFT JOIN "+pgx.Identifier{s.Schema, link.Table}.Sanitize()+" "+alias+" ON "+alias+"."+quote(link.Field)+" = r."+quote(f.Name))
				expr = "coalesce(nullif(" + alias + "." + quote(link.Caption) + ",''),r." + quote(f.Name) + "::text)"
			}
		}
		projection = append(projection, expr)
	}
	return "SELECT " + strings.Join(projection, ",") + " FROM " + pgx.Identifier{s.Schema, s.Name}.Sanitize() + " r " + strings.Join(joins, " ") + " WHERE r.object_id = ANY($1::text[])"
}
func (s Store) Read(ctx context.Context, tenant uuid.UUID, objects []Object) (Batch, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result := Batch{Columns: map[string][]Column{}, Records: []Record{}}
	if err := validateObjects(objects); err != nil {
		return result, err
	}
	groups := map[string][]string{}
	for _, o := range objects {
		groups[o.Type] = append(groups[o.Type], o.ID)
	}
	types := []string{}
	for typ := range groups {
		types = append(types, typ)
	}
	sort.Strings(types)
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	if grant, ok := access.FromContext(ctx); ok {
		if grant.Principal.TenantID != tenant {
			return result, access.Denied()
		}
		if err = access.Check(ctx, tx, grant, false); err != nil {
			return result, err
		}
	}
	found := map[Object]Record{}
	for _, typ := range types {
		var tableID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM core.model_tables WHERE tenant_id=$1 AND name=$2 AND NOT archived`, tenant, typ).Scan(&tableID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, err
		}
		settings, err := load(ctx, tx, tableID, true)
		if err != nil {
			return result, err
		}
		fields := orderedFields(settings)
		result.Columns[typ] = []Column{}
		for _, f := range fields {
			label := settings.Display[f.ID.String()].Label
			if label == "" {
				label = f.Name
			}
			result.Columns[typ] = append(result.Columns[typ], Column{f.ID.String(), label, f.Type})
		}
		rows, err := tx.Query(ctx, recordQuery(settings, fields), groups[typ])
		if err != nil {
			return result, err
		}
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				return result, err
			}
			id := fmt.Sprint(values[0])
			o := Object{typ, id}
			record := Record{Object: o, Caption: fmt.Sprint(values[1]), Found: true, Values: map[string]any{}}
			for i, f := range fields {
				value := values[i+2]
				if settings.Display[f.ID.String()].LinkID == nil {
					if label, ok := f.Enum[fmt.Sprint(value)]; ok {
						value = label
					}
				}
				record.Values[f.ID.String()] = value
			}
			found[o] = record
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
	}
	for _, o := range objects {
		r, ok := found[o]
		if !ok {
			r = Record{Object: o, Caption: o.ID, Values: map[string]any{}}
		}
		result.Records = append(result.Records, r)
	}
	return result, tx.Commit(ctx)
}
