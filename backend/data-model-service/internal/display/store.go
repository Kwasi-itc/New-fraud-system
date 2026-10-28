package display

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	DB           *pgxpool.Pool
	Transactions interface {
		RunWithTx(context.Context, func(ports.MutationStore, pgx.Tx) error) error
	}
	SetCaption func(context.Context, ports.MutationStore, uuid.UUID, string) error
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func load(ctx context.Context, db queryer, tableID uuid.UUID, projectionOnly ...bool) (Settings, error) {
	s := Settings{Fields: []Field{}, Links: []Link{}, DisplayColumns: []DisplayColumn{}}
	var rawCols []byte
	err := db.QueryRow(ctx, `SELECT t.id,t.tenant_id,t.name,n.schema_name,t.caption_field,t.display_version,
 coalesce(o.displayed_fields,'{}'::uuid[]),coalesce(o.field_order,'{}'::uuid[]),coalesce(o.field_display,'{}'::jsonb),
 coalesce(o.display_columns,'[]'::jsonb)
 FROM core.model_tables t JOIN core.tenants n ON n.id=t.tenant_id
 LEFT JOIN core.table_options o ON o.table_id=t.id AND o.tenant_id=t.tenant_id
 WHERE t.id=$1 AND NOT t.archived AND n.status='active'`, tableID).Scan(
		&s.TableID, &s.TenantID, &s.Name, &s.Schema, &s.Caption, &s.Version,
		&s.Displayed, &s.Order, &s.Display, &rawCols,
	)
	if err != nil {
		return s, err
	}
	if len(rawCols) > 0 {
		_ = json.Unmarshal(rawCols, &s.DisplayColumns)
	}

	projection := len(projectionOnly) > 0 && projectionOnly[0]

	linkFilter := ""
	linkArgs := []any{s.TableID, s.TenantID}
	if projection {
		linkIDs := []uuid.UUID{}
		for _, id := range s.Displayed {
			if choice := s.Display[id.String()]; choice.LinkID != nil {
				linkIDs = append(linkIDs, *choice.LinkID)
			}
		}
		for _, col := range s.DisplayColumns {
			if col.Type == "relation" && col.LinkID != nil {
				linkIDs = append(linkIDs, *col.LinkID)
			}
		}
		linkFilter = " AND l.id=ANY($3::uuid[])"
		linkArgs = append(linkArgs, linkIDs)
	}

	type linkRow struct {
		link          Link
		parentTableID uuid.UUID
	}
	linkRows := []linkRow{}
	parentTableIDs := map[uuid.UUID]bool{}

	rows, err := db.Query(ctx, `SELECT l.id,l.child_field_id,l.name,p.id,p.name,pf.name,coalesce(p.caption_field,'')
 FROM core.model_links l JOIN core.model_tables p ON p.id=l.parent_table_id AND p.tenant_id=l.tenant_id AND NOT p.archived
 JOIN core.model_fields pf ON pf.id=l.parent_field_id AND pf.table_id=p.id AND pf.tenant_id=l.tenant_id AND pf.is_unique AND NOT pf.archived
 JOIN core.model_fields cf ON cf.id=l.child_field_id AND cf.table_id=l.child_table_id AND cf.tenant_id=l.tenant_id AND NOT cf.archived AND cf.data_type=pf.data_type
 WHERE l.child_table_id=$1 AND l.tenant_id=$2`+linkFilter+` ORDER BY l.id`, linkArgs...)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var lr linkRow
		if err = rows.Scan(&lr.link.ID, &lr.link.FieldID, &lr.link.Name, &lr.parentTableID, &lr.link.Table, &lr.link.Field, &lr.link.Caption); err != nil {
			return s, err
		}
		lr.link.TargetFields = []Field{}
		linkRows = append(linkRows, lr)
		parentTableIDs[lr.parentTableID] = true
	}
	if err = rows.Err(); err != nil {
		return s, err
	}
	rows.Close()

	parentFieldsMap := map[uuid.UUID][]Field{}
	if len(parentTableIDs) > 0 {
		pIDs := make([]uuid.UUID, 0, len(parentTableIDs))
		for pID := range parentTableIDs {
			pIDs = append(pIDs, pID)
		}
		pRows, err := db.Query(ctx, `SELECT f.id,f.table_id,f.name,coalesce(f.alias,''),f.data_type,coalesce((SELECT jsonb_object_agg(e.value,e.label) FROM core.field_enum_values e WHERE f.is_enum AND e.field_id=f.id AND e.tenant_id=f.tenant_id),'{}'::jsonb)
 FROM core.model_fields f WHERE f.table_id=ANY($1::uuid[]) AND f.tenant_id=$2 AND NOT f.archived ORDER BY f.name`, pIDs, s.TenantID)
		if err != nil {
			return s, err
		}
		defer pRows.Close()
		for pRows.Next() {
			var f Field
			var tID uuid.UUID
			if err := pRows.Scan(&f.ID, &tID, &f.Name, &f.Alias, &f.Type, &f.Enum); err != nil {
				return s, err
			}
			parentFieldsMap[tID] = append(parentFieldsMap[tID], f)
		}
		pRows.Close()
	}

	for _, lr := range linkRows {
		if fields, ok := parentFieldsMap[lr.parentTableID]; ok {
			lr.link.TargetFields = fields
		}
		s.Links = append(s.Links, lr.link)
	}

	fieldFilter := ""
	fieldArgs := []any{s.TableID, s.TenantID}
	if projection {
		fieldIDs := append([]uuid.UUID{}, s.Displayed...)
		for _, col := range s.DisplayColumns {
			if col.Type == "direct" && col.FieldID != nil {
				fieldIDs = append(fieldIDs, *col.FieldID)
			}
		}
		for _, lr := range linkRows {
			fieldIDs = append(fieldIDs, lr.link.FieldID)
		}
		fieldFilter = " AND (f.name=$3 OR f.id=ANY($4::uuid[]))"
		fieldArgs = append(fieldArgs, s.Caption, fieldIDs)
	}

	fRows, err := db.Query(ctx, `SELECT f.id,f.name,coalesce(f.alias,''),f.data_type,coalesce((SELECT jsonb_object_agg(e.value,e.label) FROM core.field_enum_values e WHERE f.is_enum AND e.field_id=f.id AND e.tenant_id=f.tenant_id),'{}'::jsonb)
 FROM core.model_fields f WHERE f.table_id=$1 AND f.tenant_id=$2 AND NOT f.archived`+fieldFilter+` ORDER BY f.name`, fieldArgs...)
	if err != nil {
		return s, err
	}
	defer fRows.Close()
	for fRows.Next() {
		var f Field
		if err := fRows.Scan(&f.ID, &f.Name, &f.Alias, &f.Type, &f.Enum); err != nil {
			return s, err
		}
		s.Fields = append(s.Fields, f)
	}
	if err = fRows.Err(); err != nil {
		return s, err
	}
	fRows.Close()

	return s, nil
}

func (s Store) Get(ctx context.Context, tableID uuid.UUID) (Settings, error) {
	return load(ctx, s.DB, tableID)
}

func (s Store) Save(ctx context.Context, tableID uuid.UUID, in SaveRequest) (Settings, error) {
	var result Settings
	err := s.Transactions.RunWithTx(ctx, func(ms ports.MutationStore, tx pgx.Tx) error {
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
		if in.DisplayColumns == nil {
			in.DisplayColumns = []DisplayColumn{}
		}

		if len(in.DisplayColumns) > 0 && len(in.Displayed) == 0 {
			// Populate displayed_fields and field_order from direct columns for backwards compatibility
			for _, col := range in.DisplayColumns {
				if col.Type == "direct" && col.FieldID != nil {
					in.Displayed = append(in.Displayed, *col.FieldID)
					in.Order = append(in.Order, *col.FieldID)
				}
			}
		}

		displayColsJSON, err := json.Marshal(in.DisplayColumns)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `INSERT INTO core.table_options(id,tenant_id,table_id,displayed_fields,field_order,field_display,display_columns,updated_at)
  VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp()) ON CONFLICT(table_id) DO UPDATE SET displayed_fields=EXCLUDED.displayed_fields,
  field_order=EXCLUDED.field_order,field_display=EXCLUDED.field_display,display_columns=EXCLUDED.display_columns,updated_at=EXCLUDED.updated_at`,
			uuid.New(), current.TenantID, tableID, in.Displayed, in.Order, in.Display, displayColsJSON)
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

type activeColumn struct {
	ID        string
	Label     string
	Type      string
	Expr      string
	EnumMap   map[string]string
	FieldID   *uuid.UUID
	FieldName string
	Category  string
}

func resolveActiveColumns(s Settings) ([]activeColumn, []string) {
	joins := []string{}
	linkAliases := map[uuid.UUID]string{}
	fieldsByID := map[uuid.UUID]Field{}
	for _, f := range s.Fields {
		fieldsByID[f.ID] = f
	}
	linksByID := map[uuid.UUID]Link{}
	for _, l := range s.Links {
		linksByID[l.ID] = l
	}

	cols := []activeColumn{}

	if len(s.DisplayColumns) > 0 {
		for _, dc := range s.DisplayColumns {
			if len(cols) >= MaxColumns {
				break
			}
			if dc.Type == "direct" && dc.FieldID != nil {
				f, ok := fieldsByID[*dc.FieldID]
				if !ok {
					continue
				}
				lbl := dc.Label
				if lbl == "" {
					if f.Alias != "" {
						lbl = f.Alias
					} else {
						lbl = f.Name
					}
				}
				cols = append(cols, activeColumn{
					ID:        dc.ID,
					Label:     lbl,
					Type:      f.Type,
					Expr:      "r." + quote(f.Name),
					EnumMap:   f.Enum,
					FieldID:   dc.FieldID,
					FieldName: f.Name,
					Category:  "direct",
				})
			} else if dc.Type == "relation" && dc.LinkID != nil {
				link, ok := linksByID[*dc.LinkID]
				if !ok {
					continue
				}
				childField, ok := fieldsByID[link.FieldID]
				if !ok {
					continue
				}
				var targetField *Field
				if dc.TargetFieldID != nil {
					for _, tf := range link.TargetFields {
						if tf.ID == *dc.TargetFieldID {
							tfCopy := tf
							targetField = &tfCopy
							break
						}
					}
				}
				if targetField == nil && link.Caption != "" {
					for _, tf := range link.TargetFields {
						if tf.Name == link.Caption {
							tfCopy := tf
							targetField = &tfCopy
							break
						}
					}
				}
				targetFieldName := link.Caption
				targetType := "string"
				var enumMap map[string]string
				if targetField != nil {
					targetFieldName = targetField.Name
					targetType = targetField.Type
					enumMap = targetField.Enum
				}
				alias, exists := linkAliases[link.ID]
				if !exists {
					alias = fmt.Sprintf("p%d", len(linkAliases))
					linkAliases[link.ID] = alias
					joins = append(joins, fmt.Sprintf(
						"LEFT JOIN %s %s ON %s.%s = r.%s",
						pgx.Identifier{s.Schema, link.Table}.Sanitize(),
						alias,
						alias,
						quote(link.Field),
						quote(childField.Name),
					))
				}
				lbl := dc.Label
				if lbl == "" {
					targetName := targetFieldName
					if targetField != nil && targetField.Alias != "" {
						targetName = targetField.Alias
					}
					lbl = link.Table + "." + targetName
				}
				cols = append(cols, activeColumn{
					ID:        dc.ID,
					Label:     lbl,
					Type:      targetType,
					Expr:      "coalesce(nullif(" + alias + "." + quote(targetFieldName) + "::text,''), r." + quote(childField.Name) + "::text)",
					EnumMap:   enumMap,
					FieldID:   dc.FieldID,
					FieldName: childField.Name,
					Category:  "relation",
				})
			}
		}
		return cols, joins
	}

	// Fallback to legacy ordered fields and field_display
	fields := orderedFields(s)
	for i, f := range fields {
		lbl := s.Display[f.ID.String()].Label
		if lbl == "" {
			if f.Alias != "" {
				lbl = f.Alias
			} else {
				lbl = f.Name
			}
		}
		choice := s.Display[f.ID.String()]
		expr := "r." + quote(f.Name)
		colType := f.Type
		enumMap := f.Enum
		cat := "direct"
		if choice.LinkID != nil {
			if link, ok := linksByID[*choice.LinkID]; ok && link.FieldID == f.ID {
				alias := fmt.Sprintf("p%d", i)
				joins = append(joins, "LEFT JOIN "+pgx.Identifier{s.Schema, link.Table}.Sanitize()+" "+alias+" ON "+alias+"."+quote(link.Field)+" = r."+quote(f.Name))
				expr = "coalesce(nullif(" + alias + "." + quote(link.Caption) + ",''),r." + quote(f.Name) + "::text)"
				cat = "relation"
				enumMap = nil
			}
		}
		fieldID := f.ID
		cols = append(cols, activeColumn{
			ID:        f.ID.String(),
			Label:     lbl,
			Type:      colType,
			Expr:      expr,
			EnumMap:   enumMap,
			FieldID:   &fieldID,
			FieldName: f.Name,
			Category:  cat,
		})
	}
	return cols, joins
}

func recordQuery(s Settings, fields []Field) string {
	cols, joins := resolveActiveColumns(s)
	return recordQueryWithColumns(s, cols, joins)
}

func recordQueryWithColumns(s Settings, cols []activeColumn, joins []string) string {
	caption := "r.object_id"
	for _, f := range s.Fields {
		if f.Name == s.Caption && f.Type == "string" {
			caption = "coalesce(nullif(r." + quote(f.Name) + ",''),r.object_id)"
		}
	}
	projection := []string{"r.object_id", caption}
	for _, col := range cols {
		projection = append(projection, col.Expr)
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
		cols, joins := resolveActiveColumns(settings)
		result.Columns[typ] = []Column{}
		for _, col := range cols {
			result.Columns[typ] = append(result.Columns[typ], Column{
				ID:       col.ID,
				Label:    col.Label,
				Type:     col.Type,
				Category: col.Category,
			})
		}
		rows, err := tx.Query(ctx, recordQueryWithColumns(settings, cols, joins), groups[typ])
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
			for i, col := range cols {
				value := values[i+2]
				if col.EnumMap != nil {
					if label, ok := col.EnumMap[fmt.Sprint(value)]; ok {
						value = label
					}
				}
				record.Values[col.ID] = value
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
