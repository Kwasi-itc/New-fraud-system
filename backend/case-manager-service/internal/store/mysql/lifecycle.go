package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	c "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type tagRepo struct{ scope }

const tagCols = "id,tenant_id,target,name,color,deleted_at,created_at,updated_at"

func scanTag(r row) (c.Tag, error) {
	var v c.Tag
	e := r.Scan(uid(&v.ID), uid(&v.TenantID), &v.Target, &v.Name, &v.Color, &v.DeletedAt, &v.CreatedAt, &v.UpdatedAt)
	return v, normalized(e)
}
func (r tagRepo) Get(ctx context.Context, t, i uuid.UUID) (c.Tag, error) {
	return scanTag(r.db.QueryRowContext(ctx, "SELECT "+tagCols+" FROM "+r.table("tags")+" WHERE tenant_id=? AND id=? FOR SHARE", id(t), id(i)))
}
func (r tagRepo) Lock(ctx context.Context, t, i uuid.UUID) (c.Tag, error) {
	return scanTag(r.db.QueryRowContext(ctx, "SELECT "+tagCols+" FROM "+r.table("tags")+" WHERE tenant_id=? AND id=? FOR UPDATE", id(t), id(i)))
}
func (r tagRepo) Create(ctx context.Context, v c.Tag) (c.Tag, error) {
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("tags")+"("+tagCols+",active) VALUES(?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), []byte(v.Target), v.Name, v.Color, v.DeletedAt, v.CreatedAt, v.UpdatedAt, v.DeletedAt == nil)
	if e != nil {
		return c.Tag{}, normalized(e)
	}
	return r.Get(ctx, v.TenantID, v.ID)
}
func (r tagRepo) Update(ctx context.Context, v c.Tag) (c.Tag, error) {
	_, e := r.db.ExecContext(ctx, "UPDATE "+r.table("tags")+" SET target=?,name=?,color=?,updated_at=? WHERE tenant_id=? AND id=?", []byte(v.Target), v.Name, v.Color, v.UpdatedAt, id(v.TenantID), id(v.ID))
	if e != nil {
		return c.Tag{}, e
	}
	return r.Get(ctx, v.TenantID, v.ID)
}
func (r tagRepo) SoftDelete(ctx context.Context, t, i uuid.UUID, at time.Time) error {
	_, e := r.db.ExecContext(ctx, "UPDATE "+r.table("tags")+" SET active=FALSE,deleted_at=?,updated_at=? WHERE tenant_id=? AND id=?", at, at, id(t), id(i))
	return e
}
func (r tagRepo) List(ctx context.Context, t uuid.UUID, target string) ([]c.Tag, error) {
	q := "SELECT " + tagCols + " FROM " + r.table("tags") + " WHERE tenant_id=? AND active=TRUE AND deleted_at IS NULL"
	a := []any{id(t)}
	if target != "" {
		q += " AND target=?"
		a = append(a, []byte(target))
	}
	rows, e := r.db.QueryContext(ctx, q+" ORDER BY name,id LIMIT 501", a...)
	return collectLimited(rows, e, scanTag, 500)
}
func (r tagRepo) AddToCase(ctx context.Context, t, i, tag, link uuid.UUID, at time.Time) error {
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("case_tags")+"(tenant_id,case_id,tag_id,created_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE deleted_at=NULL", id(t), id(i), id(tag), at)
	return e
}
func (r tagRepo) RemoveFromCase(ctx context.Context, t, i, tag uuid.UUID, at time.Time) error {
	_, e := r.db.ExecContext(ctx, "UPDATE "+r.table("case_tags")+" SET deleted_at=? WHERE tenant_id=? AND case_id=? AND tag_id=? AND deleted_at IS NULL", at, id(t), id(i), id(tag))
	return e
}
func (r tagRepo) ListByCase(ctx context.Context, t, i uuid.UUID) ([]c.Tag, error) {
	m, e := r.ListByCases(ctx, t, []uuid.UUID{i})
	if e != nil {
		return nil, e
	}
	v := m[i]
	if v == nil {
		v = []c.Tag{}
	}
	return v, nil
}
func (r tagRepo) ListByCases(ctx context.Context, t uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]c.Tag, error) {
	out := map[uuid.UUID][]c.Tag{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 500 {
		return nil, c.Invalid("too many cases")
	}
	a := []any{id(t)}
	for _, v := range ids {
		a = append(a, id(v))
	}
	a = append(a, len(ids)*501)
	rows, e := r.db.QueryContext(ctx, "SELECT l.case_id,"+prefixColumns(tagCols, "t")+" FROM "+r.table("case_tags")+" l JOIN "+r.table("tags")+" t ON t.tenant_id=l.tenant_id AND t.id=l.tag_id WHERE l.tenant_id=? AND l.case_id IN("+placeholders(len(ids))+") AND l.deleted_at IS NULL ORDER BY l.case_id,t.name,t.id LIMIT ?", a...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var i uuid.UUID
		var v c.Tag
		e = rows.Scan(uid(&i), uid(&v.ID), uid(&v.TenantID), &v.Target, &v.Name, &v.Color, &v.DeletedAt, &v.CreatedAt, &v.UpdatedAt)
		if e != nil {
			return nil, e
		}
		if len(out[i]) == 500 {
			return nil, c.Invalid("case has more than 500 tags; narrow the selection")
		}
		out[i] = append(out[i], v)
	}
	return out, rows.Err()
}

type eventRepo struct{ scope }

func (r eventRepo) Create(ctx context.Context, v c.Event) (c.Event, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return c.Event{}, e
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("case_events")+"(id,tenant_id,case_id,event_type,payload,created_at) VALUES(?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.CaseID), v.EventType, mysqlstore.JSON(b), v.CreatedAt)
	if e != nil {
		return c.Event{}, normalized(e)
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("outbox_events")+"(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES(?,?,'case',?,?,?,'pending',?)", id(v.ID), id(v.TenantID), id(v.CaseID), v.EventType, mysqlstore.JSON(b), v.CreatedAt)
	return v, e
}
func (r eventRepo) ListByCase(ctx context.Context, t, i uuid.UUID, n int) ([]c.Event, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT id,tenant_id,case_id,event_type,payload,created_at FROM "+r.table("case_events")+" WHERE tenant_id=? AND case_id=? ORDER BY created_at DESC,id DESC LIMIT ?", id(t), id(i), bounded(n))
	return collect(rows, e, func(r row) (c.Event, error) {
		var v c.Event
		var b []byte
		var eventID, tenant, caseID uuid.UUID
		var kind string
		var at time.Time
		e := r.Scan(uid(&eventID), uid(&tenant), uid(&caseID), &kind, &b, &at)
		if e != nil {
			return v, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return v, e
		}
		v.ID, v.TenantID, v.CaseID, v.EventType, v.CreatedAt = eventID, tenant, caseID, kind, at
		return v, nil
	})
}

type fileRepo struct{ scope }

const fileCols = "id,tenant_id,case_id,file_name,content_type,file_size,storage_key,uploaded_by,created_at,source_file_id"

func scanFile(r row) (c.File, error) {
	var v c.File
	e := r.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.CaseID), &v.FileName, &v.ContentType, &v.FileSize, &v.StorageKey, &v.UploadedBy, &v.CreatedAt, nullableID{&v.SourceFileID})
	return v, normalized(e)
}
func (r fileRepo) Create(ctx context.Context, v c.File) (c.File, error) {
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("case_files")+"("+fileCols+") VALUES(?,?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.CaseID), v.FileName, v.ContentType, v.FileSize, v.StorageKey, v.UploadedBy, v.CreatedAt, idp(v.SourceFileID))
	return v, normalized(e)
}
func (r fileRepo) ListByCase(ctx context.Context, t, i uuid.UUID) ([]c.File, error) {
	rows, e := r.db.QueryContext(ctx, "SELECT "+fileCols+" FROM "+r.table("case_files")+" WHERE tenant_id=? AND case_id=? ORDER BY created_at DESC,id DESC LIMIT 501", id(t), id(i))
	return collectLimited(rows, e, scanFile, 500)
}

type uploadRepo struct{ scope }

func (r uploadRepo) Create(ctx context.Context, v c.Upload) error {
	_, e := r.db.ExecContext(ctx, "INSERT INTO "+r.table("evidence_uploads")+"(id,tenant_id,case_id,uploaded_by,file_name,content_type,file_size,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.CaseID), v.UploadedBy, v.FileName, v.ContentType, v.FileSize, v.CreatedAt, v.ExpiresAt)
	return normalized(e)
}
func (r uploadRepo) Lock(ctx context.Context, t, i, u uuid.UUID) (c.Upload, error) {
	var v c.Upload
	e := r.db.QueryRowContext(ctx, "SELECT id,tenant_id,case_id,uploaded_by,file_name,content_type,file_size,data,sha256,finalized,created_at,expires_at FROM "+r.table("evidence_uploads")+" WHERE tenant_id=? AND case_id=? AND id=? FOR UPDATE", id(t), id(i), id(u)).Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.CaseID), &v.UploadedBy, &v.FileName, &v.ContentType, &v.FileSize, &v.Data, &v.SHA256, &v.Finalized, &v.CreatedAt, &v.ExpiresAt)
	if errors.Is(e, sql.ErrNoRows) {
		return r.screeningEvidence(ctx, t, i, u)
	}
	v.StorageKey = "case-db/" + v.ID.String()
	return v, normalized(e)
}

// An imported file retains its screening identity. Read its bytes through the
// explicit owner contract after case authorization; never interpret an arbitrary
// storage URL or copy a provider credential into the case service.
func (r uploadRepo) screeningEvidence(ctx context.Context, t, caseID, fileID uuid.UUID) (c.Upload, error) {
	out := c.Upload{File: c.File{ID: fileID, TenantID: t, CaseID: caseID}}
	var source mysqlstore.NullUUID
	e := r.db.QueryRowContext(ctx, "SELECT source_file_id FROM "+r.table("case_files")+" WHERE tenant_id=? AND case_id=? AND id=?", id(t), id(caseID), id(fileID)).Scan(&source)
	if e != nil {
		return out, normalized(e)
	}
	if !source.Valid {
		return out, c.ErrNotFound
	}
	var screening uuid.UUID
	e = r.db.QueryRowContext(ctx, "SELECT f.file_name,f.content_type,f.file_size,f.storage_key,f.uploaded_by,f.created_at,s.screening_id FROM "+r.table("case_files")+" f JOIN "+r.schema+".screening_files s ON s.tenant_id=f.tenant_id AND s.id=f.source_file_id AND s.file_name=f.file_name AND s.content_type=f.content_type AND s.file_size=f.file_size AND s.storage_key=f.storage_key WHERE f.tenant_id=? AND f.case_id=? AND f.id=?", id(t), id(caseID), id(fileID)).Scan(&out.FileName, &out.ContentType, &out.FileSize, &out.StorageKey, &out.UploadedBy, &out.CreatedAt, uid(&screening))
	if e != nil {
		return out, screeningContractError(e)
	}
	raw, ok := strings.CutPrefix(out.StorageKey, "local-screening/")
	blob, e := uuid.Parse(raw)
	if !ok || e != nil || blob == uuid.Nil {
		return out, c.ErrUnavailable
	}
	var storedHash []byte
	e = r.db.QueryRowContext(ctx, "SELECT content,content_sha256 FROM "+r.schema+".screening_local_blobs WHERE tenant_id=? AND screening_id=? AND id=? AND file_name=? AND content_type=? AND file_size=? AND content IS NOT NULL FOR SHARE", id(t), id(screening), id(blob), out.FileName, out.ContentType, out.FileSize).Scan(&out.Data, &storedHash)
	if e != nil {
		return out, screeningContractError(e)
	}
	digest := sha256.Sum256(out.Data)
	if int64(len(out.Data)) != out.FileSize || !bytes.Equal(digest[:], storedHash) {
		return out, c.ErrConflict
	}
	out.SHA256 = hex.EncodeToString(storedHash)
	out.Finalized = true
	return out, nil
}
func (r uploadRepo) Save(ctx context.Context, v c.Upload) error {
	_, e := r.db.ExecContext(ctx, "UPDATE "+r.table("evidence_uploads")+" SET data=?,sha256=?,finalized=? WHERE tenant_id=? AND case_id=? AND id=?", v.Data, v.SHA256, v.Finalized, id(v.TenantID), id(v.CaseID), id(v.ID))
	return e
}

type reportRepo struct{ scope }

const reportCols = "id,tenant_id,case_id,status,version,payload,created_by,completed_by,created_at,updated_at,completed_at"

func scanReport(r row) (c.Report, error) {
	var v c.Report
	var b []byte
	e := r.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.CaseID), &v.Status, &v.Version, &b, &v.CreatedBy, &v.CompletedBy, &v.CreatedAt, &v.UpdatedAt, &v.CompletedAt)
	if e != nil {
		return v, normalized(e)
	}
	var header struct{ Format string }
	if e = json.Unmarshal(b, &header); e != nil {
		return v, e
	}
	if header.Format != "internal_sar_v1" {
		v.LegacyPayload = b
		return v, nil
	}
	e = json.Unmarshal(b, &v.Payload)
	return v, e
}
func (r reportRepo) Create(ctx context.Context, v c.Report) error {
	b, e := json.Marshal(v.Payload)
	if e != nil {
		return e
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+r.table("reports")+"("+reportCols+") VALUES(?,?,?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.CaseID), v.Status, v.Version, mysqlstore.JSON(b), v.CreatedBy, v.CompletedBy, v.CreatedAt, v.UpdatedAt, v.CompletedAt)
	return normalized(e)
}
func (r reportRepo) Get(ctx context.Context, t, i, report uuid.UUID, lock bool) (c.Report, error) {
	q := "SELECT " + reportCols + " FROM " + r.table("reports") + " WHERE tenant_id=? AND case_id=? AND id=?"
	if lock {
		q += " FOR UPDATE"
	}
	return scanReport(r.db.QueryRowContext(ctx, q, id(t), id(i), id(report)))
}
func (r reportRepo) Save(ctx context.Context, v c.Report, expected int) error {
	b, e := json.Marshal(v.Payload)
	if e != nil {
		return e
	}
	result, e := r.db.ExecContext(ctx, "UPDATE "+r.table("reports")+" SET status=?,version=?,payload=?,completed_by=?,updated_at=?,completed_at=? WHERE tenant_id=? AND case_id=? AND id=? AND version=? AND status='draft'", v.Status, v.Version, mysqlstore.JSON(b), v.CompletedBy, v.UpdatedAt, v.CompletedAt, id(v.TenantID), id(v.CaseID), id(v.ID), expected)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return c.ErrConflict
	}
	return e
}
func (r reportRepo) List(ctx context.Context, t, i uuid.UUID, before *c.Cursor, n int) (c.ReportPage, error) {
	n = bounded(n)
	out := c.ReportPage{Reports: []c.Report{}}
	q := "SELECT " + reportCols + " FROM " + r.table("reports") + " WHERE tenant_id=? AND case_id=?"
	a := []any{id(t), id(i)}
	if before != nil {
		q += " AND(created_at<? OR(created_at=? AND id<?))"
		a = append(a, before.At, before.At, id(before.ID))
	}
	a = append(a, n+1)
	rows, e := r.db.QueryContext(ctx, q+" ORDER BY created_at DESC,id DESC LIMIT ?", a...)
	out.Reports, e = collect(rows, e, scanReport)
	if e != nil {
		return out, e
	}
	if len(out.Reports) > n {
		out.Reports = out.Reports[:n]
		v := out.Reports[n-1]
		out.NextCursor = (c.Cursor{ID: v.ID, At: v.CreatedAt}).Encode()
	}
	return out, nil
}
func (r reportRepo) Evidence(ctx context.Context, t, i uuid.UUID, ids []uuid.UUID) ([]c.EvidenceReference, error) {
	out := []c.EvidenceReference{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 500 {
		return nil, c.Invalid("too many evidence references")
	}
	a := []any{id(t), id(i)}
	for _, v := range ids {
		a = append(a, id(v))
	}
	rows, e := r.db.QueryContext(ctx, "SELECT id,file_name,content_type,file_size,sha256 FROM "+r.table("evidence_uploads")+" WHERE tenant_id=? AND case_id=? AND finalized=TRUE AND id IN("+placeholders(len(ids))+") ORDER BY id", a...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v c.EvidenceReference
		if e = rows.Scan(uid(&v.ID), &v.Name, &v.ContentType, &v.Size, &v.SHA256); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
