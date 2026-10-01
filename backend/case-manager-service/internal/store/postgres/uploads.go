package postgres

import (
	"context"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
)

type UploadRepository struct{ db queryable }

func (r UploadRepository) Create(ctx context.Context, u casepkg.Upload) error {
	_, err := r.db.Exec(ctx, `INSERT INTO case_manager.evidence_uploads(id,tenant_id,case_id,uploaded_by,file_name,content_type,file_size,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, u.ID, u.TenantID, u.CaseID, u.UploadedBy, u.FileName, u.ContentType, u.FileSize, u.CreatedAt, u.ExpiresAt)
	return err
}
func (r UploadRepository) Lock(ctx context.Context, tenant, id, upload uuid.UUID) (casepkg.Upload, error) {
	var u casepkg.Upload
	err := r.db.QueryRow(ctx, `SELECT id,tenant_id,case_id,uploaded_by,file_name,content_type,file_size,data,COALESCE(sha256,''),finalized,created_at,expires_at FROM case_manager.evidence_uploads WHERE tenant_id=$1 AND case_id=$2 AND id=$3 FOR UPDATE`, tenant, id, upload).Scan(&u.ID, &u.TenantID, &u.CaseID, &u.UploadedBy, &u.FileName, &u.ContentType, &u.FileSize, &u.Data, &u.SHA256, &u.Finalized, &u.CreatedAt, &u.ExpiresAt)
	u.StorageKey = "case-db/" + u.ID.String()
	return u, err
}
func (r UploadRepository) Save(ctx context.Context, u casepkg.Upload) error {
	_, err := r.db.Exec(ctx, `UPDATE case_manager.evidence_uploads SET data=$4,sha256=$5,finalized=$6 WHERE tenant_id=$1 AND case_id=$2 AND id=$3`, u.TenantID, u.CaseID, u.ID, u.Data, u.SHA256, u.Finalized)
	return err
}
func (m Maintenance) CleanUploads(ctx context.Context, limit int) error {
	if limit < 1 || limit > 1000 {
		return casepkg.Invalid("batch limit must be 1..1000")
	}
	_, err := m.DB.Exec(ctx, `WITH expired AS (SELECT id FROM case_manager.evidence_uploads WHERE NOT finalized AND expires_at<=now() ORDER BY expires_at,id LIMIT $1 FOR UPDATE SKIP LOCKED) DELETE FROM case_manager.evidence_uploads u USING expired e WHERE u.id=e.id`, limit)
	return err
}
