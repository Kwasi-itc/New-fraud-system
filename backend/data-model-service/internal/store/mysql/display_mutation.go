package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Save atomically validates, saves both representations, maintains reverse
// dependencies, updates versions and appends audit history. effects is required
// to perform authorized owner work (including caption index scheduling) on this
// transaction. It must not commit, perform DDL/network calls or invert lock order.
// This internal callback is not an authorization bypass for API callers.
func (s *DisplayStore) Save(ctx context.Context, table uuid.UUID, in display.SaveRequest, effects func(context.Context, *sql.Tx) error) (display.Settings, error) {
	return s.save(ctx, table, in, effects, nil)
}

type displayPublication struct {
	store        *PublicationStore
	operation    PublicationOperation
	result       contract.Publication
	previous     uuid.UUID
	retainResult bool
	settings     display.Settings
}

func (s *DisplayStore) save(ctx context.Context, table uuid.UUID, in display.SaveRequest, effects func(context.Context, *sql.Tx) error, publication *displayPublication) (display.Settings, error) {
	if table == uuid.Nil || effects == nil {
		return display.Settings{}, errors.New("table and owner effects required")
	}
	if len(in.Order)+len(in.Displayed)+len(in.Display)+len(in.DisplayColumns) > s.maxRows {
		return display.Settings{}, ErrSnapshotLimit
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return display.Settings{}, err
	}
	if len(raw) > s.maxBytes {
		return display.Settings{}, ErrSnapshotLimit
	}
	var result display.Settings
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		r := s.repository(tx)
		// Relationship identity is immutable. Discover only requested parents,
		// then acquire all model-table locks in UUID order before reading state.
		linkIDs := map[uuid.UUID]bool{}
		for _, v := range in.Display {
			if v.LinkID != nil {
				linkIDs[*v.LinkID] = true
			}
		}
		for _, v := range in.DisplayColumns {
			if v.LinkID != nil {
				linkIDs[*v.LinkID] = true
			}
		}
		tables := map[uuid.UUID]bool{table: true}
		var previousCaption string
		var captionBytes int64
		if err := tx.QueryRowContext(ctx, "SELECT "+r.projection("caption_field")+" FROM "+s.schema+".data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)).Scan(&captionBytes, &previousCaption); err != nil {
			return err
		}
		if captionBytes > int64(s.maxBytes) {
			return ErrSnapshotLimit
		}
		dependents := map[uuid.UUID]bool{}
		if previousCaption != in.Caption {
			var err error
			dependents, err = s.displayDependents(ctx, tx, "data_model_display_table_references", "parent_table_id", table)
			if err != nil {
				return err
			}
		}
		for id := range dependents {
			tables[id] = true
		}
		ids := sortedDisplayIDs(linkIDs)
		for start := 0; start < len(ids); start += 1000 {
			end := min(start+1000, len(ids))
			args := []any{mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)}
			for _, id := range ids[start:end] {
				args = append(args, mysqlstore.UUID(id))
			}
			rows, err := tx.QueryContext(ctx, "SELECT parent_table_id FROM "+s.schema+".data_model_links WHERE tenant_id=? AND child_table_id=? AND id IN ("+placeholders(end-start)+")", args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id mysqlstore.UUID
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				tables[uuid.UUID(id)] = true
			}
			err = rows.Err()
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		if in.Caption == "" {
			parents, err := s.displayParents(ctx, tx, dependents)
			if err != nil {
				return err
			}
			for id := range parents {
				tables[id] = true
			}
		}
		if len(tables) > s.maxRows {
			return ErrSnapshotLimit
		}
		for _, id := range sortedDisplayIDs(tables) {
			if err := r.lock(ctx, "data_model_tables", id, "", uuid.Nil); err != nil {
				return err
			}
		}
		if publication != nil {
			prior, err := publication.store.readOperation(ctx, tx, s.tenant, table, publication.operation)
			if err == nil {
				publication.result = prior
				if publication.retainResult {
					publication.settings, err = s.readDisplayResult(ctx, tx, table, publication.operation.ID)
					return err
				}
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			// Display-only changes require a published physical model. Keep its
			// shared registry gate until commit, excluding concurrent DDL prepare.
			var token mysqlstore.UUID
			if err := tx.QueryRowContext(ctx, "SELECT version FROM "+s.schema+".data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)).Scan(&token); err != nil {
				return err
			}
			publication.previous = uuid.UUID(token)
			if err := publication.operation.Condition.Check(&contract.Publication{TenantID: s.tenant, TableID: table, Version: publication.previous}); err != nil {
				return err
			}
			registry := PhysicalRegistry{r}
			if _, err := registry.LockPublished(ctx, table, publication.previous); err != nil {
				return err
			}
		}
		current, links, err := loadDisplay(ctx, r, table)
		if err != nil {
			return err
		}
		if current.Caption != in.Caption {
			dependents, err = s.displayDependents(ctx, tx, "data_model_display_table_references", "parent_table_id", table)
			if err != nil {
				return err
			}
			for id := range dependents {
				if !tables[id] {
					return display.ErrConflict
				}
			}
			if in.Caption == "" {
				parents, err := s.displayParents(ctx, tx, dependents)
				if err != nil {
					return err
				}
				for id := range parents {
					if !tables[id] {
						return display.ErrConflict
					}
				}
			}
		}
		for id := range linkIDs {
			if l, ok := links[id]; ok && !tables[l.parent] {
				return display.ErrConflict
			}
		}
		// A self-relation sees the caption being saved, not the previous one.
		for i := range current.Links {
			if l := links[current.Links[i].ID]; l.parent == table {
				current.Links[i].Caption = in.Caption
			}
		}
		next, err := display.ValidateStrict(current, in)
		if err != nil {
			return err
		}
		increments := int64(1)
		if current.Caption != next.Caption {
			increments++
		}
		if current.Version > 9223372036854775807-increments {
			return errors.New("display version exhausted")
		}
		var at mysqlstore.DateTime
		if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&at); err != nil {
			return err
		}
		if err := (TableOptionsRepository{r}).Upsert(ctx, datamodel.TableOptions{ID: uuid.New(), TenantID: s.tenant, TableID: table, DisplayedFields: next.Displayed, FieldOrder: next.Order, UpdatedAt: at.Time}); err != nil {
			return err
		}
		jsonDisplay, err := json.Marshal(next.Display)
		if err != nil {
			return err
		}
		jsonColumns, err := json.Marshal(next.DisplayColumns)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_table_options SET field_display=?,display_columns=? WHERE tenant_id=? AND table_id=?", jsonDisplay, jsonColumns, mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".data_model_tables SET caption_field=?,display_version=?,updated_at=? WHERE tenant_id=? AND id=?", next.Caption, current.Version+increments, at, mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)); err != nil {
			return err
		}
		if current.Caption != next.Caption {
			var err error
			if next.Caption == "" {
				err = s.clearDependentCaptions(ctx, tx, table, dependents)
			} else {
				err = s.bumpDisplayDependents(ctx, tx, table, dependents)
			}
			if err != nil {
				return err
			}
		}
		if err := s.replaceDisplayReferences(ctx, tx, table, next, current.Fields, links); err != nil {
			return err
		}
		if err := effects(ctx, tx); err != nil {
			return err
		}
		details, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".data_model_schema_change_log(id,tenant_id,operation,resource_type,resource_id,status,details,created_at) VALUES (?,?,'update_display_settings','table',?,'applied',?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(s.tenant), mysqlstore.UUID(table), details, at); err != nil {
			return err
		}
		if publication != nil {
			// Receipt absence was checked under all ordered table locks, before
			// settings/effects. No duplicate can run those effects concurrently.
			publication.result, err = publication.store.MutateAndPublishOperationTx(ctx, tx, s.tenant, table, publication.operation, func(*sql.Tx) error { return nil })
			if err == nil && publication.result.Version != publication.previous {
				return ErrPhysicalConflict
			}
			if err == nil && publication.retainResult {
				publication.settings, _, err = loadDisplay(ctx, r, table)
				if err != nil {
					return err
				}
				return s.writeDisplayResult(ctx, tx, table, publication.operation.ID, publication.settings)
			}
			return err
		}
		result, _, err = loadDisplay(ctx, r, table)
		return err
	})
	if err != nil {
		return display.Settings{}, err
	}
	return result, nil
}

func (s *DisplayStore) replaceDisplayReferences(ctx context.Context, tx *sql.Tx, table uuid.UUID, in display.SaveRequest, fields []display.Field, links map[uuid.UUID]displayLink) error {
	fieldIDs, linkIDs, tableIDs := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, id := range in.Displayed {
		fieldIDs[id] = true
	}
	for _, id := range in.Order {
		fieldIDs[id] = true
	}
	for _, f := range fields {
		if in.Caption != "" && f.Name == in.Caption {
			fieldIDs[f.ID] = true
		}
	}
	for key, v := range in.Display {
		id, err := uuid.Parse(key)
		if err != nil {
			return err
		}
		fieldIDs[id] = true
		if v.LinkID != nil {
			linkIDs[*v.LinkID] = true
		}
	}
	for _, v := range in.DisplayColumns {
		if v.FieldID != nil {
			fieldIDs[*v.FieldID] = true
		}
		if v.LinkID != nil {
			linkIDs[*v.LinkID] = true
		}
		if v.TargetFieldID != nil {
			fieldIDs[*v.TargetFieldID] = true
		}
	}
	for id := range linkIDs {
		l, ok := links[id]
		if !ok {
			return errors.New("display dependency relationship missing")
		}
		fieldIDs[l.value.FieldID] = true
		fieldIDs[l.parentField] = true
		tableIDs[l.parent] = true
		// Parent caption dependency is represented by tableIDs. Its current
		// field may change without changing this child's saved configuration.
	}
	if len(fieldIDs)+len(linkIDs)+len(tableIDs) > s.maxRows {
		return ErrSnapshotLimit
	}
	for i, relation := range []string{"data_model_display_field_references", "data_model_display_link_references", "data_model_display_table_references"} {
		column := []string{"field_id", "link_id", "parent_table_id"}[i]
		set := []map[uuid.UUID]bool{fieldIDs, linkIDs, tableIDs}[i]
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+s.schema+"."+relation+" WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(table)); err != nil {
			return err
		}
		ids := sortedDisplayIDs(set)
		for start := 0; start < len(ids); start += 1000 {
			end := min(start+1000, len(ids))
			args := make([]any, 0, (end-start)*3)
			for _, id := range ids[start:end] {
				args = append(args, mysqlstore.UUID(s.tenant), mysqlstore.UUID(table), mysqlstore.UUID(id))
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+"."+relation+"(tenant_id,table_id,"+column+") VALUES "+strings.TrimSuffix(strings.Repeat("(?,?,?),", end-start), ","), args...); err != nil {
				return err
			}
		}
	}
	return nil
}
