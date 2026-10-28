package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Discover dependents through the reverse index, not a tenant JSON scan. The
// caller must lock the owner and discovered tables, then recheck: a Save may
// have added a dependency between initial discovery and acquisition of the
// owner lock. New edges abort the operation, with no automatic command retry.
func (s *DisplayStore) displayDependents(ctx context.Context, tx *sql.Tx, relation, column string, id uuid.UUID) (map[uuid.UUID]bool, error) {
	r := s.repository(tx)
	values, err := metadataList(ctx, r, "SELECT table_id FROM "+s.schema+"."+relation+" WHERE tenant_id=? AND "+column+"=? ORDER BY table_id", []any{mysqlstore.UUID(s.tenant), mysqlstore.UUID(id)}, func(row metadataScanner) (uuid.UUID, int64, error) {
		var id mysqlstore.UUID
		err := row.Scan(&id)
		return uuid.UUID(id), 0, err
	})
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]bool, len(values))
	for _, id := range values {
		result[id] = true
	}
	return result, nil
}

// Rebuilding remaining relationships also touches their parent field/table FKs.
// Lock those parents before any child settings, avoiding child-to-parent lock
// inversion against concurrent caption and field owner mutations.
func (s *DisplayStore) displayParents(ctx context.Context, tx *sql.Tx, tables map[uuid.UUID]bool) (map[uuid.UUID]bool, error) {
	r := s.repository(tx)
	ids := sortedDisplayIDs(tables)
	parents := map[uuid.UUID]bool{}
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{mysqlstore.UUID(s.tenant)}
		for _, id := range ids[start:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		values, err := metadataList(ctx, r, "SELECT DISTINCT parent_table_id FROM "+s.schema+".data_model_display_table_references WHERE tenant_id=? AND table_id IN ("+placeholders(end-start)+") ORDER BY parent_table_id", args, func(row metadataScanner) (uuid.UUID, int64, error) {
			var id mysqlstore.UUID
			err := row.Scan(&id)
			return uuid.UUID(id), 0, err
		})
		if err != nil {
			return nil, err
		}
		for _, id := range values {
			parents[id] = true
		}
		if len(parents) > s.maxRows {
			return nil, ErrSnapshotLimit
		}
	}
	return parents, nil
}

func (s *DisplayStore) bumpDisplayDependents(ctx context.Context, tx *sql.Tx, source uuid.UUID, ids map[uuid.UUID]bool) error {
	selected := make(map[uuid.UUID]bool, len(ids))
	for id := range ids {
		if id != source {
			selected[id] = true
		}
	}
	ordered := sortedDisplayIDs(selected)
	for start := 0; start < len(ordered); start += 1000 {
		end := min(start+1000, len(ordered))
		args := []any{mysqlstore.UUID(s.tenant)}
		for _, id := range ordered[start:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		result, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_tables SET display_version=display_version+1 WHERE tenant_id=? AND id IN ("+placeholders(end-start)+") AND display_version<9223372036854775807", args...)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != int64(end-start) {
			return errors.New("dependent display version exhausted or table missing")
		}
	}
	return nil
}

func (s *DisplayStore) clearDependentCaptions(ctx context.Context, tx *sql.Tx, parent uuid.UUID, dependents map[uuid.UUID]bool) error {
	selected := make(map[uuid.UUID]bool, len(dependents))
	for id := range dependents {
		if id != parent {
			selected[id] = true
		}
	}
	if len(selected) == 0 {
		return nil
	}
	r := s.repository(tx)
	links, err := metadataList(ctx, r, "SELECT id FROM "+s.schema+".data_model_links WHERE tenant_id=? AND parent_table_id=? ORDER BY id", []any{mysqlstore.UUID(s.tenant), mysqlstore.UUID(parent)}, func(row metadataScanner) (uuid.UUID, int64, error) {
		var id mysqlstore.UUID
		err := row.Scan(&id)
		return uuid.UUID(id), 0, err
	})
	if err != nil {
		return err
	}
	remove := map[uuid.UUID]bool{}
	for _, id := range links {
		remove[id] = true
	}
	settings, err := s.loadCleanupSettings(ctx, tx, selected)
	if err != nil {
		return err
	}
	for i := range settings {
		for key, choice := range settings[i].Display {
			if choice.LinkID != nil && remove[*choice.LinkID] {
				choice.LinkID = nil
				settings[i].Display[key] = choice
			}
		}
	}
	if err := s.persistCleanup(ctx, tx, settings, uuid.Nil); err != nil {
		return err
	}
	return s.rebuildDisplayReferences(ctx, tx, selected)
}
