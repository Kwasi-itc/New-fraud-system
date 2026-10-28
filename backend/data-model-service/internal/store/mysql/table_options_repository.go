package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// TableOptionsRepository persists the legacy ordered-field options. It preserves
// display JSON owned by the display protocol; complete JSON validation and
// archive/link cleanup must be supplied by that protocol before runtime use.
type TableOptionsRepository struct{ metadataRepository }

func (r TableOptionsRepository) GetByTableID(ctx context.Context, id uuid.UUID) (*datamodel.TableOptions, error) {
	// UNION ALL avoids multiplying displayed and ordering rows. Parent and both
	// lists are observed in one statement snapshot, including empty options.
	query := "SELECT o.id,o.tenant_id,o.table_id,o.updated_at,c.kind,c.ordinal,c.field_id FROM " + r.schema + ".data_model_table_options o LEFT JOIN (SELECT tenant_id,table_id,0 AS kind,ordinal,field_id FROM " + r.schema + ".data_model_displayed_fields WHERE tenant_id=? AND table_id=? UNION ALL SELECT tenant_id,table_id,1 AS kind,ordinal,field_id FROM " + r.schema + ".data_model_field_order WHERE tenant_id=? AND table_id=?) c ON c.tenant_id=o.tenant_id AND c.table_id=o.table_id WHERE o.tenant_id=? AND o.table_id=? ORDER BY c.kind,c.ordinal LIMIT ?"
	rows, err := r.tx.QueryContext(ctx, query, mysqlstore.UUID(r.tenant), mysqlstore.UUID(id), mysqlstore.UUID(r.tenant), mysqlstore.UUID(id), mysqlstore.UUID(r.tenant), mysqlstore.UUID(id), r.maxRows+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result *datamodel.TableOptions
	count := 0
	for rows.Next() {
		count++
		if count > r.maxRows {
			return nil, ErrSnapshotLimit
		}
		var v datamodel.TableOptions
		var at mysqlstore.DateTime
		var kind, ordinal sql.NullInt64
		var field mysqlstore.NullUUID
		if err := rows.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.TableID), &at, &kind, &ordinal, &field); err != nil {
			return nil, err
		}
		if result == nil {
			v.UpdatedAt = at.Time
			v.DisplayedFields = []uuid.UUID{}
			v.FieldOrder = []uuid.UUID{}
			result = &v
		}
		if kind.Valid != field.Valid || ordinal.Valid != field.Valid {
			return nil, errors.New("incomplete ordered option row")
		}
		if field.Valid {
			var target *[]uuid.UUID
			switch kind.Int64 {
			case 0:
				target = &result.DisplayedFields
			case 1:
				target = &result.FieldOrder
			default:
				return nil, errors.New("invalid ordered option kind")
			}
			if ordinal.Int64 != int64(len(*target)) {
				return nil, errors.New("noncontiguous option ordinals")
			}
			*target = append(*target, uuid.UUID(field.UUID))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r TableOptionsRepository) Upsert(ctx context.Context, v datamodel.TableOptions) error {
	if err := r.validate(v.TenantID, v.ID); err != nil {
		return err
	}
	if v.TableID == uuid.Nil {
		return errors.New("options table required")
	}
	if len(v.DisplayedFields) > 12 {
		return errors.New("at most 12 displayed fields allowed")
	}
	if len(v.DisplayedFields) > r.maxRows || len(v.FieldOrder) > r.maxRows-len(v.DisplayedFields) {
		return ErrSnapshotLimit
	}
	if _, err := (mysqlstore.DateTime{Time: v.UpdatedAt}).Value(); err != nil {
		return err
	}
	requested := make(map[uuid.UUID]struct{})
	for _, ids := range [][]uuid.UUID{v.DisplayedFields, v.FieldOrder} {
		seen := make(map[uuid.UUID]struct{}, len(ids))
		for _, id := range ids {
			if id == uuid.Nil {
				return errors.New("option field identity required")
			}
			if _, ok := seen[id]; ok {
				return errors.New("option fields must be distinct within each list")
			}
			seen[id] = struct{}{}
			requested[id] = struct{}{}
		}
	}
	// The exact model-table lock coordinates first insertion, replacement and
	// version increment. No tenant-wide lock or DDL is performed.
	var version int64
	if err := r.tx.QueryRowContext(ctx, "SELECT display_version FROM "+r.schema+".data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID)).Scan(&version); err != nil {
		return err
	}
	if version == 9223372036854775807 {
		return errors.New("display version exhausted")
	}
	ids := make([]uuid.UUID, 0, len(requested))
	for id := range requested {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	// Shared locks keep active-field validation stable until owner commit. All
	// field batches use the same UUID order; archive cleanup remains owner work.
	for offset := 0; offset < len(ids); offset += 500 {
		end := min(offset+500, len(ids))
		args := []any{mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID)}
		for _, id := range ids[offset:end] {
			args = append(args, mysqlstore.UUID(id))
		}
		query := "SELECT id,archived FROM " + r.schema + ".data_model_fields WHERE tenant_id=? AND table_id=? AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", end-offset), ",") + ") ORDER BY id FOR SHARE"
		if err := r.validateOptionFields(ctx, query, args, end-offset); err != nil {
			return err
		}
	}
	var existing mysqlstore.UUID
	err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".data_model_table_options WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID)).Scan(&existing)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_table_options(id,tenant_id,table_id,updated_at) VALUES (?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID), mysqlstore.DateTime{Time: v.UpdatedAt})
	case err == nil:
		_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_table_options SET updated_at=? WHERE tenant_id=? AND table_id=?", mysqlstore.DateTime{Time: v.UpdatedAt}, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID))
	}
	if err != nil {
		return err
	}
	for i, table := range []string{"data_model_displayed_fields", "data_model_field_order"} {
		if _, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+"."+table+" WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID)); err != nil {
			return err
		}
		values := v.DisplayedFields
		if i == 1 {
			values = v.FieldOrder
		}
		for offset := 0; offset < len(values); offset += 1000 {
			end := min(offset+1000, len(values))
			args := make([]any, 0, (end-offset)*4)
			for j := offset; j < end; j++ {
				args = append(args, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID), j, mysqlstore.UUID(values[j]))
			}
			if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+"."+table+"(tenant_id,table_id,ordinal,field_id) VALUES "+strings.TrimSuffix(strings.Repeat("(?,?,?,?),", end-offset), ","), args...); err != nil {
				return err
			}
		}
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".data_model_tables SET display_version=? WHERE tenant_id=? AND id=?", version+1, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.TableID))
	return err
}

func (r TableOptionsRepository) validateOptionFields(ctx context.Context, query string, args []any, want int) error {
	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id mysqlstore.UUID
		var archived bool
		if err := rows.Scan(&id, &archived); err != nil {
			return err
		}
		if archived {
			return errors.New("option field is archived")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != want {
		return errors.New("option field does not belong to tenant/table")
	}
	return nil
}
