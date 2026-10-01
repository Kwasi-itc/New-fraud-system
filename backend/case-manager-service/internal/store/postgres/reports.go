package postgres

import (
	"context"
	"encoding/json"
	"errors"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ReportRepository struct{ db queryable }

const reportColumns = `id,tenant_id,case_id,status,version,payload,created_by,completed_by,created_at,updated_at,completed_at`

func scanReport(row pgx.Row) (casepkg.Report, error) {
	var r casepkg.Report
	var payload []byte
	err := row.Scan(&r.ID, &r.TenantID, &r.CaseID, &r.Status, &r.Version, &payload, &r.CreatedBy, &r.CompletedBy, &r.CreatedAt, &r.UpdatedAt, &r.CompletedAt)
	if err != nil {
		return r, err
	}
	var header struct {
		Format string `json:"format"`
	}
	if json.Unmarshal(payload, &header) != nil || header.Format != "internal_sar_v1" {
		r.LegacyPayload = payload
		return r, nil
	}
	err = json.Unmarshal(payload, &r.Payload)
	return r, err
}
func (r ReportRepository) Create(ctx context.Context, v casepkg.Report) error {
	payload, err := json.Marshal(v.Payload)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO case_manager.suspicious_activity_reports (`+reportColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, v.ID, v.TenantID, v.CaseID, v.Status, v.Version, payload, v.CreatedBy, v.CompletedBy, v.CreatedAt, v.UpdatedAt, v.CompletedAt)
	return err
}
func (r ReportRepository) Get(ctx context.Context, tenant, id, report uuid.UUID, lock bool) (casepkg.Report, error) {
	query := `SELECT ` + reportColumns + ` FROM case_manager.suspicious_activity_reports WHERE tenant_id=$1 AND case_id=$2 AND id=$3`
	if lock {
		query += ` FOR UPDATE`
	}
	v, err := scanReport(r.db.QueryRow(ctx, query, tenant, id, report))
	if errors.Is(err, pgx.ErrNoRows) {
		err = casepkg.ErrNotFound
	}
	return v, err
}
func (r ReportRepository) List(ctx context.Context, tenant, id uuid.UUID, before *casepkg.Cursor, limit int) (casepkg.ReportPage, error) {
	result := casepkg.ReportPage{Reports: []casepkg.Report{}}
	query := `SELECT ` + reportColumns + ` FROM case_manager.suspicious_activity_reports WHERE tenant_id=$1 AND case_id=$2`
	args := []any{tenant, id}
	limitParameter := "$3"
	if before != nil {
		query += ` AND (created_at,id)<($3,$4)`
		args = append(args, before.At, before.ID)
		limitParameter = "$5"
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ` + limitParameter
	args = append(args, limit+1)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanReport(rows)
		if err != nil {
			return result, err
		}
		result.Reports = append(result.Reports, v)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Reports) > limit {
		result.Reports = result.Reports[:limit]
		v := result.Reports[limit-1]
		result.NextCursor = (casepkg.Cursor{ID: v.ID, At: v.CreatedAt}).Encode()
	}
	return result, nil
}
func (r ReportRepository) Save(ctx context.Context, v casepkg.Report, expected int) error {
	payload, err := json.Marshal(v.Payload)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `UPDATE case_manager.suspicious_activity_reports SET status=$4,version=$5,payload=$6,completed_by=$7,updated_at=$8,completed_at=$9 WHERE tenant_id=$1 AND case_id=$2 AND id=$3 AND version=$10 AND status='draft'`, v.TenantID, v.CaseID, v.ID, v.Status, v.Version, payload, v.CompletedBy, v.UpdatedAt, v.CompletedAt, expected)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return casepkg.ErrConflict
	}
	return nil
}
func (r ReportRepository) Evidence(ctx context.Context, tenant, id uuid.UUID, ids []uuid.UUID) ([]casepkg.EvidenceReference, error) {
	result := []casepkg.EvidenceReference{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(ctx, `SELECT id,file_name,content_type,file_size,sha256 FROM case_manager.evidence_uploads WHERE tenant_id=$1 AND case_id=$2 AND id=ANY($3::uuid[]) AND finalized ORDER BY id`, tenant, id, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v casepkg.EvidenceReference
		if err := rows.Scan(&v.ID, &v.Name, &v.ContentType, &v.Size, &v.SHA256); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) != len(ids) {
		return nil, casepkg.Invalid("attachments must be finalized evidence in this case")
	}
	return result, nil
}
