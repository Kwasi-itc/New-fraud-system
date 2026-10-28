package mysql

import (
	"context"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// EventCursor is a position, not an authorization token. Repositories validate
// its tenant and stream; callers must still authorize each request.
type EventCursor struct {
	TenantID uuid.UUID
	Stream   string
	At       time.Time
	ID       uuid.UUID
}

type SchemaChangeRepository struct{ metadataRepository }
type TenantSchemaMigrationRepository struct{ metadataRepository }

func (r SchemaChangeRepository) Create(ctx context.Context, v datamodel.SchemaChange) error {
	if err := r.validate(v.TenantID, v.ID, v.Operation, v.ResourceType, v.Status); err != nil {
		return err
	}
	// Reject oversized payloads before the JSON valuer allocates its string.
	remaining := r.maxBytes - len(v.Operation) - len(v.ResourceType) - len(v.Status)
	if len(v.Details) > remaining {
		return ErrSnapshotLimit
	}
	if v.ResourceID == uuid.Nil {
		return errors.New("audit resource identity required")
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_schema_change_log(id,tenant_id,operation,resource_type,resource_id,status,details,created_at) VALUES (?,?,?,?,?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), v.Operation, v.ResourceType, mysqlstore.UUID(v.ResourceID), v.Status, mysqlstore.JSON(v.Details), mysqlstore.DateTime{Time: v.CreatedAt})
	return err
}

func (r SchemaChangeRepository) selectSQL() string {
	return "SELECT id,tenant_id,resource_id," + r.projection("operation", "resource_type", "status", "CAST(details AS CHAR CHARACTER SET utf8mb4)") + ",created_at FROM " + r.schema + ".data_model_schema_change_log WHERE tenant_id=?"
}

func scanHistory(row metadataScanner) (datamodel.SchemaChange, int64, error) {
	var v datamodel.SchemaChange
	var size int64
	var created mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.ResourceID), &size, &v.Operation, &v.ResourceType, &v.Status, &v.Details, &created)
	v.CreatedAt = created.Time
	return v, size, err
}

func (r SchemaChangeRepository) ListByTenant(ctx context.Context, id uuid.UUID) ([]datamodel.SchemaChange, error) {
	if err := r.validate(id, id); err != nil {
		return nil, err
	}
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" ORDER BY created_at DESC,id DESC", []any{mysqlstore.UUID(r.tenant)}, scanHistory)
}

func (r SchemaChangeRepository) ListPage(ctx context.Context, limit int, after *EventCursor) ([]datamodel.SchemaChange, *EventCursor, error) {
	return eventPage(ctx, r.metadataRepository, r.selectSQL(), "created_at", "history", limit, after, scanHistory, func(v datamodel.SchemaChange) (time.Time, uuid.UUID) { return v.CreatedAt, v.ID })
}

func (r TenantSchemaMigrationRepository) Create(ctx context.Context, v datamodel.TenantSchemaMigration) error {
	if err := r.validate(v.TenantID, v.ID, v.Version); err != nil {
		return err
	}
	_, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".data_model_tenant_schema_migrations(id,tenant_id,version,applied_at) VALUES (?,?,?,?)", mysqlstore.UUID(v.ID), mysqlstore.UUID(v.TenantID), v.Version, mysqlstore.DateTime{Time: v.AppliedAt})
	return err
}

func (r TenantSchemaMigrationRepository) selectSQL() string {
	return "SELECT id,tenant_id," + r.projection("version") + ",applied_at FROM " + r.schema + ".data_model_tenant_schema_migrations WHERE tenant_id=?"
}

func scanRevision(row metadataScanner) (datamodel.TenantSchemaMigration, int64, error) {
	var v datamodel.TenantSchemaMigration
	var size int64
	var applied mysqlstore.DateTime
	err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), &size, &v.Version, &applied)
	v.AppliedAt = applied.Time
	return v, size, err
}

func (r TenantSchemaMigrationRepository) ListByTenant(ctx context.Context, id uuid.UUID) ([]datamodel.TenantSchemaMigration, error) {
	if err := r.validate(id, id); err != nil {
		return nil, err
	}
	return metadataList(ctx, r.metadataRepository, r.selectSQL()+" ORDER BY applied_at DESC,id DESC", []any{mysqlstore.UUID(r.tenant)}, scanRevision)
}

func (r TenantSchemaMigrationRepository) ListPage(ctx context.Context, limit int, after *EventCursor) ([]datamodel.TenantSchemaMigration, *EventCursor, error) {
	return eventPage(ctx, r.metadataRepository, r.selectSQL(), "applied_at", "revisions", limit, after, scanRevision, func(v datamodel.TenantSchemaMigration) (time.Time, uuid.UUID) { return v.AppliedAt, v.ID })
}

// eventPage uses fixed private SQL identifiers, the existing tenant/time/UUID
// indexes and a sentinel row. A byte-limit error returns no partial page/cursor.
func eventPage[T any](ctx context.Context, r metadataRepository, query, column, stream string, limit int, after *EventCursor, scan func(metadataScanner) (T, int64, error), position func(T) (time.Time, uuid.UUID)) ([]T, *EventCursor, error) {
	if limit < 1 || limit > r.maxRows {
		return nil, nil, errors.New("page limit outside configured row budget")
	}
	args := []any{mysqlstore.UUID(r.tenant)}
	if after != nil {
		if after.TenantID != r.tenant || after.Stream != stream || after.ID == uuid.Nil {
			return nil, nil, errors.New("invalid event cursor scope")
		}
		at := mysqlstore.DateTime{Time: after.At}
		if _, err := at.Value(); err != nil {
			return nil, nil, err
		}
		query += " AND (" + column + "<? OR (" + column + "=? AND id<?))"
		args = append(args, at, at, mysqlstore.UUID(after.ID))
	}
	query += " ORDER BY " + column + " DESC,id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	var used int64
	var more bool
	for rows.Next() {
		if len(result) == limit {
			more = true
			continue
		}
		v, size, err := scan(rows)
		if err != nil {
			return nil, nil, err
		}
		if size > int64(r.maxBytes)-used {
			return nil, nil, ErrSnapshotLimit
		}
		used += size
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if more {
		at, id := position(result[len(result)-1])
		return result, &EventCursor{TenantID: r.tenant, Stream: stream, At: at, ID: id}, nil
	}
	return result, nil, nil
}
