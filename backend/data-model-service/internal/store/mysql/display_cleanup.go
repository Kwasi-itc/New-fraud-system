package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// RemoveReferences coordinates field archive/delete or link deletion with all
// affected display settings. effects must perform that exact metadata mutation
// plus owner audit/publication on this transaction, without DDL or network I/O.
// It is not selected by existing metadata services until all owner writers use
// this protocol. kind is deliberately restricted to field and link.
func (s *DisplayStore) RemoveReferences(ctx context.Context, kind string, id uuid.UUID, effects func(context.Context, *sql.Tx) error) error {
	if (kind != "field" && kind != "link") || id == uuid.Nil || effects == nil {
		return errors.New("field/link identity and owner mutation required")
	}
	return mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		owner, name, links, affected, err := s.removalScope(ctx, tx, kind, id)
		if err != nil {
			return err
		}
		r := s.repository(tx)
		locked, err := s.displayParents(ctx, tx, affected)
		if err != nil {
			return err
		}
		for table := range affected {
			locked[table] = true
		}
		if len(locked) > s.maxRows {
			return ErrSnapshotLimit
		}
		for _, table := range sortedDisplayIDs(locked) {
			if err := r.lock(ctx, "data_model_tables", table, "", uuid.Nil); err != nil {
				return err
			}
		}
		currentOwner, currentName, currentLinks, current, err := s.removalScope(ctx, tx, kind, id)
		if err != nil {
			return err
		}
		if currentOwner != owner || currentName != name {
			return display.ErrConflict
		}
		for table := range current {
			if !locked[table] {
				return display.ErrConflict
			}
		}
		parents, err := s.displayParents(ctx, tx, current)
		if err != nil {
			return err
		}
		for table := range parents {
			if !locked[table] {
				return display.ErrConflict
			}
		}
		links, affected = currentLinks, current
		settings, err := s.loadCleanupSettings(ctx, tx, affected)
		if err != nil {
			return err
		}
		captionLinks := map[uuid.UUID]bool{}
		if kind == "field" {
			for _, v := range settings {
				if v.TableID == owner && v.Caption == name {
					values, err := metadataList(ctx, r, "SELECT id FROM "+s.schema+".data_model_links WHERE tenant_id=? AND parent_table_id=? ORDER BY id", []any{mysqlstore.UUID(s.tenant), mysqlstore.UUID(owner)}, func(row metadataScanner) (uuid.UUID, int64, error) {
						var id mysqlstore.UUID
						err := row.Scan(&id)
						return uuid.UUID(id), 0, err
					})
					if err != nil {
						return err
					}
					for _, id := range values {
						captionLinks[id] = true
					}
				}
			}
		}
		for i := range settings {
			v := &settings[i]
			if kind == "field" {
				v.Displayed = withoutDisplayField(v.Displayed, id)
				v.Order = withoutDisplayField(v.Order, id)
				delete(v.Display, id.String())
				if v.TableID == owner && v.Caption == name {
					v.Caption = ""
				}
			}
			for key, choice := range v.Display {
				if choice.LinkID != nil && (links[*choice.LinkID] || captionLinks[*choice.LinkID]) {
					choice.LinkID = nil
					v.Display[key] = choice
				}
			}
			columns := make([]display.DisplayColumn, 0, len(v.DisplayColumns))
			for _, col := range v.DisplayColumns {
				removed := col.LinkID != nil && links[*col.LinkID]
				if kind == "field" {
					removed = removed || col.FieldID != nil && *col.FieldID == id || col.TargetFieldID != nil && *col.TargetFieldID == id
				}
				if !removed {
					columns = append(columns, col)
				}
			}
			v.DisplayColumns = columns
		}
		if err := s.persistCleanup(ctx, tx, settings, owner); err != nil {
			return err
		}
		if err := s.rebuildDisplayReferences(ctx, tx, affected); err != nil {
			return err
		}
		if err := effects(ctx, tx); err != nil {
			return err
		}
		relation, predicate := "data_model_links", ""
		if kind == "field" {
			relation, predicate = "data_model_fields", " AND NOT archived"
		}
		var unfinished int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+s.schema+"."+relation+" WHERE tenant_id=? AND id=?"+predicate, mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)).Scan(&unfinished); err != nil {
			return err
		}
		if unfinished != 0 {
			return errors.New("display cleanup owner mutation did not archive/delete its resource")
		}
		return nil
	})
}

func withoutDisplayField(ids []uuid.UUID, remove uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != remove {
			out = append(out, id)
		}
	}
	return out
}

func (s *DisplayStore) removalScope(ctx context.Context, tx *sql.Tx, kind string, id uuid.UUID) (uuid.UUID, string, map[uuid.UUID]bool, map[uuid.UUID]bool, error) {
	r := s.repository(tx)
	var owner mysqlstore.UUID
	name := ""
	links := map[uuid.UUID]bool{}
	affected := map[uuid.UUID]bool{}
	fail := func(err error) (uuid.UUID, string, map[uuid.UUID]bool, map[uuid.UUID]bool, error) {
		return uuid.Nil, "", nil, nil, err
	}
	if kind == "field" {
		var n int64
		if err := tx.QueryRowContext(ctx, "SELECT table_id,"+r.projection("name")+" FROM "+s.schema+".data_model_fields WHERE tenant_id=? AND id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)).Scan(&owner, &n, &name); err != nil {
			return fail(err)
		}
		if n > int64(s.maxBytes) {
			return fail(ErrSnapshotLimit)
		}
		values, err := metadataList(ctx, r, "SELECT id,child_table_id FROM "+s.schema+".data_model_links WHERE tenant_id=? AND ((parent_table_id=? AND parent_field_id=?) OR (child_table_id=? AND child_field_id=?)) ORDER BY id", []any{mysqlstore.UUID(s.tenant), owner, mysqlstore.UUID(id), owner, mysqlstore.UUID(id)}, func(row metadataScanner) ([2]uuid.UUID, int64, error) {
			var a, b mysqlstore.UUID
			err := row.Scan(&a, &b)
			return [2]uuid.UUID{uuid.UUID(a), uuid.UUID(b)}, 0, err
		})
		if err != nil {
			return fail(err)
		}
		for _, v := range values {
			links[v[0]] = true
			affected[v[1]] = true
		}
		refs, err := s.displayDependents(ctx, tx, "data_model_display_field_references", "field_id", id)
		if err != nil {
			return fail(err)
		}
		for table := range refs {
			affected[table] = true
		}
		// Parent caption fields are represented by a table dependency because
		// the selected caption can change independently of child configuration.
		var caption bool
		if err := tx.QueryRowContext(ctx, "SELECT BINARY caption_field=BINARY ? FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=?", name, mysqlstore.UUID(s.tenant), owner).Scan(&caption); err != nil {
			return fail(err)
		}
		if caption {
			refs, err := s.displayDependents(ctx, tx, "data_model_display_table_references", "parent_table_id", uuid.UUID(owner))
			if err != nil {
				return fail(err)
			}
			for table := range refs {
				affected[table] = true
			}
		}
	} else {
		if err := tx.QueryRowContext(ctx, "SELECT child_table_id FROM "+s.schema+".data_model_links WHERE tenant_id=? AND id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)).Scan(&owner); err != nil {
			return fail(err)
		}
		links[id] = true
	}
	ids := sortedDisplayIDs(links)
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{mysqlstore.UUID(s.tenant)}
		for _, id := range ids[start:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		values, err := metadataList(ctx, r, "SELECT DISTINCT table_id FROM "+s.schema+".data_model_display_link_references WHERE tenant_id=? AND link_id IN ("+placeholders(end-start)+") ORDER BY table_id", args, func(row metadataScanner) (uuid.UUID, int64, error) {
			var id mysqlstore.UUID
			err := row.Scan(&id)
			return uuid.UUID(id), 0, err
		})
		if err != nil {
			return fail(err)
		}
		for _, table := range values {
			affected[table] = true
		}
		if len(affected) > s.maxRows {
			return fail(ErrSnapshotLimit)
		}
	}
	affected[uuid.UUID(owner)] = true
	if len(affected) > s.maxRows {
		return fail(ErrSnapshotLimit)
	}
	return uuid.UUID(owner), name, links, affected, nil
}

type cleanupSettings struct {
	display.Settings
	hasOptions bool
}

func (s *DisplayStore) loadCleanupSettings(ctx context.Context, tx *sql.Tx, tables map[uuid.UUID]bool) ([]cleanupSettings, error) {
	r := s.repository(tx)
	ids := sortedDisplayIDs(tables)
	result := make([]cleanupSettings, 0, len(ids))
	byID := map[uuid.UUID]int{}
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{mysqlstore.UUID(s.tenant)}
		for _, id := range ids[start:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		values, err := displayList(ctx, &r, "SELECT t.id,t.display_version,o.id IS NOT NULL,"+r.projection("t.caption_field", "COALESCE(CAST(o.field_display AS CHAR),'{}')", "COALESCE(CAST(o.display_columns AS CHAR),'[]')")+" FROM "+s.schema+".data_model_tables t LEFT JOIN "+s.schema+".data_model_table_options o ON o.tenant_id=t.tenant_id AND o.table_id=t.id WHERE t.tenant_id=? AND t.id IN ("+placeholders(end-start)+") ORDER BY t.id", args, func(row metadataScanner) (cleanupSettings, int64, error) {
			v := cleanupSettings{Settings: display.Settings{Displayed: []uuid.UUID{}, Order: []uuid.UUID{}, Display: map[string]display.FieldDisplay{}, DisplayColumns: []display.DisplayColumn{}}}
			var n int64
			var raw, cols string
			err := row.Scan((*mysqlstore.UUID)(&v.TableID), &v.Version, &v.hasOptions, &n, &v.Caption, &raw, &cols)
			if err == nil && n <= int64(r.maxBytes) {
				err = json.Unmarshal([]byte(raw), &v.Display)
				if err == nil {
					err = json.Unmarshal([]byte(cols), &v.DisplayColumns)
				}
			}
			return v, n, err
		})
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			r.maxRows -= len(v.Display) + len(v.DisplayColumns)
			if r.maxRows < 0 {
				return nil, ErrSnapshotLimit
			}
			byID[v.TableID] = len(result)
			result = append(result, v)
		}
		for _, relation := range []string{"data_model_displayed_fields", "data_model_field_order"} {
			_, err := displayList(ctx, &r, "SELECT table_id,ordinal,field_id FROM "+s.schema+"."+relation+" WHERE tenant_id=? AND table_id IN ("+placeholders(end-start)+") ORDER BY table_id,ordinal", args, func(row metadataScanner) (struct{}, int64, error) {
				var table, field mysqlstore.UUID
				var ordinal int
				if err := row.Scan(&table, &ordinal, &field); err != nil {
					return struct{}{}, 0, err
				}
				i, ok := byID[uuid.UUID(table)]
				if !ok {
					return struct{}{}, 0, errors.New("cleanup table missing")
				}
				list := &result[i].Displayed
				if relation == "data_model_field_order" {
					list = &result[i].Order
				}
				if ordinal != len(*list) {
					return struct{}{}, 0, errors.New("noncontiguous cleanup options")
				}
				*list = append(*list, uuid.UUID(field))
				return struct{}{}, 0, nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	if len(result) != len(ids) {
		return nil, sql.ErrNoRows
	}
	return result, nil
}

func (s *DisplayStore) persistCleanup(ctx context.Context, tx *sql.Tx, settings []cleanupSettings, owner uuid.UUID) error {
	// Metadata was loaded in bounded batches. Writes preserve each JSON array's
	// order and atomically replace ordinal rows without transient unique conflicts.
	for _, v := range settings {
		increments := int64(1)
		if v.hasOptions && v.TableID == owner {
			increments++
		}
		if v.Version > 9223372036854775807-increments {
			return errors.New("display version exhausted")
		}
		if v.hasOptions {
			raw, err := json.Marshal(v.Display)
			if err != nil {
				return err
			}
			cols, err := json.Marshal(v.DisplayColumns)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_table_options SET field_display=?,display_columns=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND table_id=?", raw, cols, mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.TableID)); err != nil {
				return err
			}
			for i, relation := range []string{"data_model_displayed_fields", "data_model_field_order"} {
				if _, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+"."+relation+" WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.TableID)); err != nil {
					return err
				}
				list := v.Displayed
				if i == 1 {
					list = v.Order
				}
				for start := 0; start < len(list); start += 1000 {
					end := min(start+1000, len(list))
					args := make([]any, 0, (end-start)*4)
					for j := start; j < end; j++ {
						args = append(args, mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.TableID), j, mysqlstore.UUID(list[j]))
					}
					if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+"."+relation+"(tenant_id,table_id,ordinal,field_id) VALUES "+strings.TrimSuffix(strings.Repeat("(?,?,?,?),", end-start), ","), args...); err != nil {
						return err
					}
				}
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_tables SET caption_field=?,display_version=? WHERE tenant_id=? AND id=?", v.Caption, v.Version+increments, mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.TableID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *DisplayStore) rebuildDisplayReferences(ctx context.Context, tx *sql.Tx, tables map[uuid.UUID]bool) error {
	ids := sortedDisplayIDs(tables)
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{mysqlstore.UUID(s.tenant)}
		for _, id := range ids[start:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		filter := "tenant_id=? AND table_id IN (" + placeholders(end-start) + ")"
		for _, relation := range []string{"data_model_display_field_references", "data_model_display_link_references", "data_model_display_table_references"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+"."+relation+" WHERE "+filter, args...); err != nil {
				return err
			}
		}
		options := s.schema + ".data_model_table_options o"
		optionFilter := "o.tenant_id=? AND o.table_id IN (" + placeholders(end-start) + ")"
		// JSON_TABLE extracts only UUID references already validated by Save.
		// Malformed imported state fails explicitly; migration must backfill and
		// validate dependencies before this adapter is activated.
		linkSQL := "SELECT o.tenant_id,o.table_id,UUID_TO_BIN(j.link_id) FROM " + options + " JOIN JSON_TABLE(o.display_columns,'$[*]' COLUMNS(link_id CHAR(36) PATH '$.link_id')) j WHERE j.link_id IS NOT NULL AND " + optionFilter + " UNION SELECT o.tenant_id,o.table_id,UUID_TO_BIN(j.link_id) FROM " + options + " JOIN JSON_TABLE(o.field_display,'$.*' COLUMNS(link_id CHAR(36) PATH '$.link_id')) j WHERE j.link_id IS NOT NULL AND " + optionFilter
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_display_link_references(tenant_id,table_id,link_id) "+linkSQL, append(append([]any{}, args...), args...)...); err != nil {
			return fmt.Errorf("rebuild display link references: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_display_table_references(tenant_id,table_id,parent_table_id) SELECT DISTINCT r.tenant_id,r.table_id,l.parent_table_id FROM (SELECT * FROM "+s.schema+".data_model_display_link_references WHERE "+filter+") r JOIN "+s.schema+".data_model_links l ON l.tenant_id=r.tenant_id AND l.id=r.link_id", args...); err != nil {
			return err
		}
		parts := []string{
			"SELECT tenant_id,table_id,field_id FROM " + s.schema + ".data_model_displayed_fields WHERE " + filter,
			"SELECT tenant_id,table_id,field_id FROM " + s.schema + ".data_model_field_order WHERE " + filter,
			"SELECT o.tenant_id,o.table_id,UUID_TO_BIN(j.id) FROM " + options + " JOIN JSON_TABLE(JSON_KEYS(o.field_display),'$[*]' COLUMNS(id CHAR(36) PATH '$')) j WHERE " + optionFilter,
			"SELECT o.tenant_id,o.table_id,UUID_TO_BIN(j.id) FROM " + options + " JOIN JSON_TABLE(o.display_columns,'$[*]' COLUMNS(id CHAR(36) PATH '$.field_id')) j WHERE j.id IS NOT NULL AND " + optionFilter,
			"SELECT o.tenant_id,o.table_id,UUID_TO_BIN(j.id) FROM " + options + " JOIN JSON_TABLE(o.display_columns,'$[*]' COLUMNS(id CHAR(36) PATH '$.target_field_id')) j WHERE j.id IS NOT NULL AND " + optionFilter,
			"SELECT r.tenant_id,r.table_id,l.parent_field_id FROM (SELECT * FROM " + s.schema + ".data_model_display_link_references WHERE " + filter + ") r JOIN " + s.schema + ".data_model_links l ON l.tenant_id=r.tenant_id AND l.id=r.link_id",
			"SELECT r.tenant_id,r.table_id,l.child_field_id FROM (SELECT * FROM " + s.schema + ".data_model_display_link_references WHERE " + filter + ") r JOIN " + s.schema + ".data_model_links l ON l.tenant_id=r.tenant_id AND l.id=r.link_id",
			"SELECT t.tenant_id,t.id,f.id FROM " + s.schema + ".data_model_tables t JOIN " + s.schema + ".data_model_fields f ON f.tenant_id=t.tenant_id AND f.table_id=t.id AND BINARY f.name=BINARY t.caption_field WHERE t.tenant_id=? AND t.id IN (" + placeholders(end-start) + ") AND OCTET_LENGTH(t.caption_field)>0",
		}
		allArgs := make([]any, 0, len(args)*len(parts))
		for range parts {
			allArgs = append(allArgs, args...)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_display_field_references(tenant_id,table_id,field_id) "+strings.Join(parts, " UNION "), allArgs...); err != nil {
			return fmt.Errorf("rebuild display field references: %w", err)
		}
	}
	return nil
}
