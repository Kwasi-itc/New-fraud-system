package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Detail returns bounded transport metadata under current owner authorization.
// It never transfers accepted payloads, receipt keys or result JSON.
func (w *WorkOwner) Detail(ctx context.Context, id uuid.UUID, kind string) (UploadSummary, error) {
	scope, err := w.resourceScope(ctx, id)
	if err != nil {
		return UploadSummary{}, err
	}
	if err = w.owner.receipts.authorize(ctx, scope); err != nil {
		return UploadSummary{}, err
	}
	var r UploadSummary
	err = mysqlstore.Mutation(ctx, w.owner.receipts.db, func(tx *sql.Tx) error {
		if err := w.owner.receipts.authorizeTx(ctx, tx, scope); err != nil {
			return err
		}
		var mode string
		var created mysqlstore.DateTime
		var started, finished mysqlstore.NullDateTime
		var filename, content sql.NullString
		var errorCode sql.NullString
		q := "SELECT r.id,r.mode,CASE WHEN OCTET_LENGTH(r.filename)<=4096 THEN r.filename ELSE NULL END,CASE WHEN OCTET_LENGTH(r.content_type)<=4096 THEN r.content_type ELSE NULL END,j.state,j.attempt,r.total_rows,r.successful_rows,r.failed_rows,r.created_at,j.started_at,COALESCE(r.completed_at,j.finished_at),j.last_error FROM " + w.owner.receipts.schema + ".ingestion_work_resources r JOIN " + w.owner.receipts.schema + ".ingestion_work_jobs j ON j.tenant_id=r.tenant_id AND j.resource_id=r.id AND j.id=COALESCE(r.current_job_id,r.id) WHERE r.tenant_id=? AND r.table_id=? AND r.id=? AND r.kind=?"
		var key mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, q, mysqlstore.UUID(scope.TenantID), mysqlstore.UUID(scope.TableID), mysqlstore.UUID(id), kind).Scan(&key, &mode, &filename, &content, &r.State, &r.Attempt, &r.TotalRows, &r.SuccessfulRows, &r.FailedRows, &created, &started, &finished, &errorCode); err != nil {
			return err
		}
		if !filename.Valid || !content.Valid {
			return errors.New("work metadata exceeds bound")
		}
		r.ID = uuid.UUID(key)
		r.Mode = ingestion.Mode(mode)
		r.Filename = filename.String
		r.ContentType = content.String
		r.RequestedAt = created.Time
		if started.Valid && r.State != "available" {
			r.StartedAt = &started.DateTime.Time
		}
		if finished.Valid {
			r.CompletedAt = &finished.DateTime.Time
		}
		if errorCode.Valid {
			r.ErrorCode = &errorCode.String
		}
		return nil
	})
	if err != nil {
		return UploadSummary{}, err
	}
	return r, nil
}
