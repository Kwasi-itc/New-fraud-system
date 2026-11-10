package mysql

import (
	"context"
	"encoding/json"
	"errors"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/google/uuid"
	"math"
	"strings"
)

const matchColumns = `id,tenant_id,screening_id,entity_id,provider,status,name,score,payload,matched_texts,unique_counterparty_identifier,enriched,created_at,updated_at`

func scanMatch(row scanner) (d.Match, error) {
	var v d.Match
	var texts []byte
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.ScreeningID), &v.EntityID, &v.Provider, &v.Status, &v.Name, &v.Score, &v.Payload, &texts, &v.UniqueCounterpartyIdentifier, &v.Enriched, &v.CreatedAt, &v.UpdatedAt)
	if e == nil {
		e = json.Unmarshal(texts, &v.MatchedTexts)
	}
	return v, normalize(e)
}
func (r matches) ReplaceForScreening(ctx context.Context, s string, items []d.Match) error {
	if len(items) > 500 {
		return ErrLimit
	}
	var tenant string
	if e := r.q.QueryRowContext(ctx, "SELECT tenant_id FROM "+r.table("screenings")+" WHERE id=? FOR UPDATE", id(s)).Scan(uid(&tenant)); e != nil {
		return normalize(e)
	}
	// Parent locking and fencing belong to the enclosing dispatch transaction.
	for _, v := range items {
		if v.ScreeningID != s || v.TenantID != tenant || math.IsNaN(v.Score) || math.IsInf(v.Score, 0) || len(v.Payload) > 1<<20 {
			return errors.New("invalid screening matches")
		}
	}
	if _, e := r.q.ExecContext(ctx, "DELETE FROM "+r.table("matches")+" WHERE tenant_id=? AND screening_id=?", id(tenant), id(s)); e != nil {
		return normalize(e)
	}
	for _, v := range items {
		b, e := encoded(v.MatchedTexts)
		if e != nil {
			return e
		}
		_, e = r.q.ExecContext(ctx, "INSERT INTO "+r.table("matches")+"(tenant_id,screening_id,id,entity_id,provider,name,status,score,payload,matched_texts,unique_counterparty_identifier,enriched,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)", id(tenant), id(s), id(matchKey(v.ID)), v.EntityID, v.Provider, v.Name, v.Status, v.Score, jsonValue(v.Payload), b, v.UniqueCounterpartyIdentifier, v.Enriched, v.CreatedAt, v.UpdatedAt)
		if e != nil {
			return normalize(e)
		}
	}
	return nil
}
func (r matches) ListByScreening(ctx context.Context, t, s string) ([]d.Match, error) {
	return list(ctx, r.Store, "SELECT "+matchColumns+" FROM "+r.table("matches")+" WHERE tenant_id=? AND screening_id=? ORDER BY created_at,id LIMIT 501", []any{id(t), id(s)}, 500, scanMatch)
}
func (r matches) GetByID(ctx context.Context, t, m string) (d.Match, error) {
	return scanMatch(r.q.QueryRowContext(ctx, "SELECT "+matchColumns+" FROM "+r.table("matches")+" WHERE tenant_id=? AND id=?", id(t), id(m)))
}
func (r matches) Update(ctx context.Context, v d.Match) (d.Match, error) {
	b, e := encoded(v.MatchedTexts)
	if e != nil {
		return v, e
	}
	_, e = r.q.ExecContext(ctx, "UPDATE "+r.table("matches")+" SET status=?,name=?,score=?,payload=?,matched_texts=?,unique_counterparty_identifier=?,enriched=?,updated_at=? WHERE tenant_id=? AND screening_id=? AND id=?", v.Status, v.Name, v.Score, jsonValue(v.Payload), b, v.UniqueCounterpartyIdentifier, v.Enriched, v.UpdatedAt, id(v.TenantID), id(v.ScreeningID), id(v.ID))
	if e != nil {
		return d.Match{}, normalize(e)
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r matches) ReviewCounts(ctx context.Context, t, s string) (int, int, error) {
	var pending, confirmed int
	e := r.q.QueryRowContext(ctx, "SELECT COALESCE(SUM(status='pending'),0),COALESCE(SUM(status='confirmed_hit'),0) FROM "+r.table("matches")+" WHERE tenant_id=? AND screening_id=?", id(t), id(s)).Scan(&pending, &confirmed)
	return pending, confirmed, normalize(e)
}
func scanComment(row scanner) (d.Comment, error) {
	var v d.Comment
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.MatchID), &v.CommentText, &v.AuthorID, &v.CreatedAt)
	return v, normalize(e)
}
func (r comments) Create(ctx context.Context, v d.Comment) (d.Comment, error) {
	_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("comments")+"(tenant_id,id,match_id,comment_text,author_id,created_at) VALUES(?,?,?,?,?,?)", id(v.TenantID), id(v.ID), id(v.MatchID), v.CommentText, v.AuthorID, v.CreatedAt)
	return v, normalize(e)
}
func (r comments) ListByMatchIDs(ctx context.Context, t string, ids []string) ([]d.Comment, error) {
	if len(ids) == 0 {
		return []d.Comment{}, nil
	}
	if len(ids) > 500 {
		return nil, ErrLimit
	}
	a := []any{id(t)}
	for _, v := range ids {
		a = append(a, id(v))
	}
	return list(ctx, r.Store, "SELECT id,tenant_id,match_id,comment_text,author_id,created_at FROM "+r.table("comments")+" WHERE tenant_id=? AND match_id IN("+placeholders(len(ids))+") ORDER BY created_at,id LIMIT 1001", a, 1000, scanComment)
}
func scanWhitelist(row scanner) (d.WhitelistEntry, error) {
	var v d.WhitelistEntry
	var reason string
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), &v.EntityID, &v.UniqueCounterpartyIdentifier, &reason, &v.ReviewerID, &v.CreatedAt)
	return v, normalize(e)
}
func (r whitelist) Create(ctx context.Context, v d.WhitelistEntry) (d.WhitelistEntry, error) {
	_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("whitelist")+"(tenant_id,id,entity_id,counterparty_identifier,reason,created_by,created_at) VALUES(?,?,?,?,?,?,?)", id(v.TenantID), id(v.ID), []byte(v.EntityID), v.UniqueCounterpartyIdentifier, "", v.ReviewerID, v.CreatedAt)
	return v, normalize(e)
}
func (r whitelist) Delete(ctx context.Context, t, entity string, cp *string) error {
	_, e := r.q.ExecContext(ctx, "DELETE FROM "+r.table("whitelist")+" WHERE tenant_id=? AND entity_id=? AND counterparty_identifier<=>?", id(t), []byte(entity), cp)
	return normalize(e)
}
func (r whitelist) Search(ctx context.Context, t string, entity, cp *string) ([]d.WhitelistEntry, error) {
	q := "SELECT id,tenant_id,entity_id,counterparty_identifier,reason,created_by,created_at FROM " + r.table("whitelist") + " WHERE tenant_id=?"
	a := []any{id(t)}
	if entity != nil {
		q += " AND entity_id=?"
		a = append(a, []byte(*entity))
	}
	if cp != nil {
		q += " AND counterparty_identifier=?"
		a = append(a, []byte(*cp))
	}
	return list(ctx, r.Store, q+" ORDER BY created_at,id LIMIT 501", a, 500, scanWhitelist)
}
func scanFile(row scanner) (d.File, error) {
	var v d.File
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.ScreeningID), &v.FileName, &v.ContentType, &v.FileSize, &v.StorageKey, &v.UploadedBy, &v.CreatedAt)
	return v, normalize(e)
}

const fileColumns = `id,tenant_id,screening_id,file_name,content_type,file_size,storage_key,uploaded_by,created_at`

func (r files) Create(ctx context.Context, v d.File) (d.File, error) {
	key, ok := strings.CutPrefix(v.StorageKey, "local-screening/")
	blob, e := uuid.Parse(key)
	if !ok || e != nil || blob == uuid.Nil {
		return v, errors.New("screening-owned upload session required")
	}
	var owned bool
	e = r.q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.table("local_blobs")+" WHERE tenant_id=? AND screening_id=? AND id=? AND file_name=? AND content_type=? AND file_size=?)", id(v.TenantID), id(v.ScreeningID), id(blob.String()), v.FileName, v.ContentType, v.FileSize).Scan(&owned)
	if e != nil {
		return v, normalize(e)
	}
	if !owned {
		return v, errors.New("evidence upload ownership or metadata mismatch")
	}
	_, e = r.q.ExecContext(ctx, "INSERT INTO "+r.table("files")+"(id,tenant_id,screening_id,file_name,content_type,file_size,storage_key,uploaded_by,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.ScreeningID), v.FileName, v.ContentType, v.FileSize, []byte(v.StorageKey), v.UploadedBy, v.CreatedAt)
	return v, normalize(e)
}
func (r files) GetByID(ctx context.Context, t, s, f string) (d.File, error) {
	return scanFile(r.q.QueryRowContext(ctx, "SELECT "+fileColumns+" FROM "+r.table("files")+" WHERE tenant_id=? AND screening_id=? AND id=?", id(t), id(s), id(f)))
}
func (r files) ListByScreening(ctx context.Context, t, s string) ([]d.File, error) {
	return list(ctx, r.Store, "SELECT "+fileColumns+" FROM "+r.table("files")+" WHERE tenant_id=? AND screening_id=? ORDER BY created_at,id LIMIT 501", []any{id(t), id(s)}, 500, scanFile)
}
