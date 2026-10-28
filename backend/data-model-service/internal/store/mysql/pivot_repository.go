package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type PivotRepository struct{ metadataRepository }

func (r PivotRepository) Create(ctx context.Context, v datamodel.Pivot) error {
	if err := r.validate(v.TenantID, v.ID); err != nil {
		return err
	}
	if err := datamodel.ValidatePivot(v.FieldID, v.PathLinkIDs); err != nil {
		return err
	}
	if v.BaseTableID == uuid.Nil || (v.FieldID != nil && *v.FieldID == uuid.Nil) {
		return errors.New("pivot base and nonzero optional field required")
	}
	if len(v.PathLinkIDs) > r.maxRows {
		return ErrSnapshotLimit
	}
	for _, id := range v.PathLinkIDs {
		if id == uuid.Nil {
			return errors.New("pivot link identity required")
		}
	}
	field := mysqlstore.NullUUID{}
	if v.FieldID != nil {
		field = mysqlstore.NullUUID{UUID: mysqlstore.UUID(*v.FieldID), Valid: true}
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_pivots(id,tenant_id,base_table_id,field_id,created_at) VALUES (?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.BaseTableID), field, mysqlstore.DateTime{Time: v.CreatedAt}); err != nil {
		return err
	}
	// Bound parameter count and statement size independently of model capacity.
	// On any child error the caller must roll back the WHOLE owner transaction.
	for offset := 0; offset < len(v.PathLinkIDs); offset += 1000 {
		end := min(offset+1000, len(v.PathLinkIDs))
		args := make([]any, 0, (end-offset)*4)
		for i := offset; i < end; i++ {
			args = append(args, mysqlstore.UUID(r.tenant), mysqlstore.UUID(v.ID), i, mysqlstore.UUID(v.PathLinkIDs[i]))
		}
		values := strings.TrimSuffix(strings.Repeat("(?,?,?,?),", end-offset), ",")
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_pivot_path_links(tenant_id,pivot_id,ordinal,link_id) VALUES "+values, args...); err != nil {
			return err
		}
	}
	return nil
}

// A single joined statement gives one coherent view of parents and ordered
// children, even under READ COMMITTED. maxRows bounds joined rows, not just
// parent count, and overflow never returns a partially assembled path.
func (r PivotRepository) read(ctx context.Context, id *uuid.UUID) ([]datamodel.Pivot, error) {
	query := "SELECT p.id,p.tenant_id,p.base_table_id,p.field_id,p.created_at,l.ordinal,l.link_id FROM " + r.schema + ".data_model_pivots p LEFT JOIN " + r.schema + ".data_model_pivot_path_links l ON l.tenant_id=p.tenant_id AND l.pivot_id=p.id WHERE p.tenant_id=?"
	args := []any{mysqlstore.UUID(r.tenant)}
	if id != nil {
		query += " AND p.id=?"
		args = append(args, mysqlstore.UUID(*id))
	}
	query += " ORDER BY p.created_at DESC,p.id DESC,l.ordinal LIMIT ?"
	args = append(args, r.maxRows+1)
	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]datamodel.Pivot, 0)
	count := 0
	for rows.Next() {
		count++
		if count > r.maxRows {
			return nil, ErrSnapshotLimit
		}
		var v datamodel.Pivot
		var field, link mysqlstore.NullUUID
		var created mysqlstore.DateTime
		var ordinal sql.NullInt64
		if err := rows.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.BaseTableID), &field, &created, &ordinal, &link); err != nil {
			return nil, err
		}
		v.CreatedAt = created.Time
		v.PathLinkIDs = []uuid.UUID{}
		if field.Valid {
			value := uuid.UUID(field.UUID)
			v.FieldID = &value
		}
		if len(result) == 0 || result[len(result)-1].ID != v.ID {
			result = append(result, v)
		}
		current := &result[len(result)-1]
		if ordinal.Valid != link.Valid {
			return nil, errors.New("incomplete pivot path row")
		}
		if link.Valid {
			if ordinal.Int64 != int64(len(current.PathLinkIDs)) {
				return nil, errors.New("noncontiguous pivot path ordinals")
			}
			current.PathLinkIDs = append(current.PathLinkIDs, uuid.UUID(link.UUID))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, v := range result {
		if err := datamodel.ValidatePivot(v.FieldID, v.PathLinkIDs); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r PivotRepository) GetByID(ctx context.Context, id uuid.UUID) (datamodel.Pivot, error) {
	values, err := r.read(ctx, &id)
	if err != nil {
		return datamodel.Pivot{}, err
	}
	if len(values) == 0 {
		return datamodel.Pivot{}, sql.ErrNoRows
	}
	return values[0], nil
}

func (r PivotRepository) ListByTenant(ctx context.Context, id uuid.UUID) ([]datamodel.Pivot, error) {
	if err := r.validate(id, id); err != nil {
		return nil, err
	}
	return r.read(ctx, nil)
}

func (r PivotRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.delete(ctx, "data_model_pivots", id)
}
