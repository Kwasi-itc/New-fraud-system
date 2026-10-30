package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// UploadCursor is a position, never authorization. Bind it to the owner scope so
// it cannot accidentally be reused across tenants or a recreated table.
type UploadCursor struct {
	TenantID, TableID, ID uuid.UUID
	CreatedAt             time.Time
}

// UploadSummary deliberately contains neither payload/results nor request keys.
// State and ErrorCode are internal queue values; transport DTO mapping is separate.
type UploadSummary struct {
	ID                                             uuid.UUID
	Mode                                           ingestion.Mode
	Filename, ContentType, State                   string
	ErrorCode                                      *string
	Attempt, TotalRows, SuccessfulRows, FailedRows int
	RequestedAt                                    time.Time
	StartedAt, CompletedAt                         *time.Time
}
type UploadPage struct {
	Items []UploadSummary
	Next  *UploadCursor
}

// ListUploads requires both original write scopes, consistent with Get's current
// conservative read policy. A future independent read authorizer must be wired
// explicitly, not inferred from a caller-supplied tenant. One bounded query joins
// status by exact job identity; a missing job is corruption, not a hidden row.
func (w *WorkOwner) ListUploads(ctx context.Context, limit int, cursor *UploadCursor) (UploadPage, error) {
	if w == nil || limit < 1 || limit > 100 {
		return UploadPage{}, errors.New("upload page limit must be 1..100")
	}
	if cursor != nil {
		if cursor.TenantID != w.owner.receipts.tenant || cursor.TableID != w.owner.table || cursor.ID == uuid.Nil {
			return UploadPage{}, errors.New("upload cursor scope mismatch")
		}
		if _, err := (mysqlstore.DateTime{Time: cursor.CreatedAt}).Value(); err != nil {
			return UploadPage{}, err
		}
		// Detach mutable caller memory before authorization callbacks.
		detached := *cursor
		cursor = &detached
	}
	scopes := []ReceiptRequest{w.scope(ingestion.ModeCreate, UploadWork), w.scope(ingestion.ModePatch, UploadWork)}
	for _, scope := range scopes {
		if err := w.owner.receipts.authorize(ctx, scope); err != nil {
			return UploadPage{}, err
		}
	}
	page := UploadPage{Items: make([]UploadSummary, 0, limit)}
	err := mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		for _, scope := range scopes {
			if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
				return err
			}
		}
		query, args := w.uploadListQuery(limit+1, cursor)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item UploadSummary
			var id mysqlstore.UUID
			var requested mysqlstore.DateTime
			var started, completed mysqlstore.NullDateTime
			var name, typeName, errorCode sql.NullString
			var nameBytes, typeBytes int
			if err := rows.Scan(&id, &item.Mode, &nameBytes, &name, &typeBytes, &typeName, &item.State, &item.Attempt, &item.TotalRows, &item.SuccessfulRows, &item.FailedRows, &requested, &started, &completed, &errorCode); err != nil {
				return err
			}
			if nameBytes+typeBytes > 4096 || !name.Valid || !typeName.Valid {
				return errors.New("retained upload metadata exceeds limit")
			}
			item.ID = uuid.UUID(id)
			item.Filename = name.String
			item.ContentType = typeName.String
			item.RequestedAt = requested.Time
			if started.Valid && item.State != "available" {
				at := started.DateTime.Time
				item.StartedAt = &at
			}
			if completed.Valid {
				at := completed.DateTime.Time
				item.CompletedAt = &at
			}
			if errorCode.Valid && errorCode.String != "" {
				value := errorCode.String
				item.ErrorCode = &value
			}
			if len(page.Items) == limit {
				last := page.Items[limit-1]
				page.Next = &UploadCursor{TenantID: w.owner.receipts.tenant, TableID: w.owner.table, ID: last.ID, CreatedAt: last.RequestedAt}
				continue
			}
			page.Items = append(page.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return UploadPage{}, err
	}
	return page, nil
}

func (w *WorkOwner) uploadListQuery(limit int, cursor *UploadCursor) (string, []any) {
	s := w.owner.receipts.schema
	// EXPLAIN on mixed history chose the older kind-agnostic history index.
	// Require the scope/kind/order index so unrelated work cannot dominate a page.
	q := "SELECT r.id,r.mode,OCTET_LENGTH(r.filename),CASE WHEN OCTET_LENGTH(r.filename)<=4096 THEN r.filename ELSE NULL END,OCTET_LENGTH(r.content_type),CASE WHEN OCTET_LENGTH(r.content_type)<=4096 THEN r.content_type ELSE NULL END,j.state,j.attempt,r.total_rows,r.successful_rows,r.failed_rows,r.created_at,j.started_at,COALESCE(r.completed_at,j.finished_at),j.last_error FROM " + s + ".ingestion_work_resources r FORCE INDEX (upload_history) LEFT JOIN " + s + ".ingestion_work_jobs j ON j.id=COALESCE(r.current_job_id,r.id) AND j.tenant_id=r.tenant_id AND j.resource_id=r.id WHERE r.tenant_id=? AND r.table_id=? AND r.kind=?"
	args := []any{mysqlstore.UUID(w.owner.receipts.tenant), mysqlstore.UUID(w.owner.table), UploadWork}
	if cursor != nil {
		q += " AND (r.created_at<? OR (r.created_at=? AND r.id<?))"
		at := mysqlstore.DateTime{Time: cursor.CreatedAt}
		args = append(args, at, at, mysqlstore.UUID(cursor.ID))
	}
	q += " ORDER BY r.created_at DESC,r.id DESC LIMIT ?"
	return q, append(args, limit)
}
