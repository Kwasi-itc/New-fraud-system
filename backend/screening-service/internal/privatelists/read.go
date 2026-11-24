package privatelists

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrExpired = errors.New("private_artifact_expired")

func (s Service) List(ctx context.Context, p identity.Principal, after string) (json.RawMessage, error) {
	if err := s.Policy.check(ctx, s.DB, p, "read", false); err != nil {
		return nil, err
	}
	if len(after) > 80 {
		return nil, ErrInput
	}
	rows, err := s.DB.Query(ctx, `SELECT jsonb_build_object('id',l.dataset_id,'name',l.name,'purpose',l.purpose,'rights_reference',l.rights_reference,'archived',l.archived,'generation',d.active_generation,'record_count',coalesce(v.record_count,0),'created_at',l.created_at) FROM screening.private_lists l JOIN screening.source_datasets d ON(d.owner_scope,d.id)=(l.tenant_id,l.dataset_id) LEFT JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation) WHERE l.tenant_id=$1 AND l.dataset_id>$2 ORDER BY l.dataset_id LIMIT 51`, p.TenantID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var item []byte
		if err = rows.Scan(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	next := ""
	if more {
		var last struct {
			ID string `json:"id"`
		}
		json.Unmarshal(items[len(items)-1], &last)
		next = last.ID
	}
	return json.Marshal(map[string]any{"lists": items, "has_more": more, "next_cursor": next})
}
func (s Service) Detail(ctx context.Context, p identity.Principal, dataset string, before *uuid.UUID) (json.RawMessage, error) {
	if err := s.Policy.check(ctx, s.DB, p, "read", false); err != nil {
		return nil, err
	}
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT jsonb_build_object('id',l.dataset_id,'name',l.name,'purpose',l.purpose,'rights_reference',l.rights_reference,'archived',l.archived,'generation',d.active_generation,'record_count',coalesce(v.record_count,0)) FROM screening.private_lists l JOIN screening.source_datasets d ON(d.owner_scope,d.id)=(l.tenant_id,l.dataset_id) LEFT JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation) WHERE l.tenant_id=$1 AND l.dataset_id=$2`, p.TenantID, dataset).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT jsonb_build_object('id',i.id,'action',i.action,'status',i.status,'generation',i.generation,'published_generation',i.published_generation,'record_count',i.record_count,'created_by',i.created_by,'created_by_name',coalesce(u.display_name,'Unavailable'),'reviewed_by',i.reviewed_by,'reviewed_by_name',reviewer.display_name,'created_at',i.created_at,'reviewed_at',i.reviewed_at,'content_hash',i.content_hash,'artifact_sha256',i.artifact_sha256,'diagnostic',i.diagnostic) FROM screening.private_imports i LEFT JOIN identity.users u ON u.id=i.created_by LEFT JOIN identity.users reviewer ON reviewer.id=i.reviewed_by WHERE i.tenant_id=$1 AND i.dataset_id=$2 AND ($3::uuid IS NULL OR (i.created_at,i.id)<(SELECT created_at,id FROM screening.private_imports anchor WHERE anchor.tenant_id=$1 AND anchor.dataset_id=$2 AND anchor.id=$3)) ORDER BY i.created_at DESC,i.id DESC LIMIT 51`, p.TenantID, dataset, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var item []byte
		if err = rows.Scan(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	var next any
	if more {
		var last struct {
			ID string `json:"id"`
		}
		json.Unmarshal(items[len(items)-1], &last)
		next = last.ID
	}
	var list map[string]any
	json.Unmarshal(raw, &list)
	return json.Marshal(map[string]any{"list": list, "imports": items, "has_more": more, "next_cursor": next})
}

func (s Service) Artifact(ctx context.Context, p identity.Principal, dataset string, id uuid.UUID) ([]byte, string, error) {
	if _, err := s.GetImport(ctx, p, dataset, id); err != nil {
		return nil, "", err
	}
	var body []byte
	var digest string
	err := s.DB.QueryRow(ctx, `SELECT a.body,a.sha256 FROM screening.private_imports i JOIN screening.source_artifacts a ON(a.owner_scope,a.dataset_id,a.sha256)=(i.tenant_id,i.dataset_id,i.artifact_sha256) WHERE i.tenant_id=$1 AND i.dataset_id=$2 AND i.id=$3`, p.TenantID, dataset, id).Scan(&body, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrExpired
	}
	return body, digest, err
}

func (s Service) Diff(ctx context.Context, p identity.Principal, dataset string, id uuid.UUID, after string) (sourceingest.DiffPage, error) {
	item, err := s.GetImport(ctx, p, dataset, id)
	if err != nil {
		return sourceingest.DiffPage{}, err
	}
	if item.Generation == nil || len(after) > 200 {
		return sourceingest.DiffPage{}, ErrInput
	}
	var parent *uuid.UUID
	if err = s.DB.QueryRow(ctx, `SELECT parent_generation FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3`, p.TenantID, dataset, *item.Generation).Scan(&parent); err != nil {
		return sourceingest.DiffPage{}, err
	}
	from := uuid.Nil
	if parent != nil {
		from = *parent
	}
	return s.scoped(p, "read").Diff(ctx, sourceingest.Key{Owner: p.TenantID, Dataset: dataset}, from, *item.Generation, after, 100)
}

func (s Service) Reject(ctx context.Context, p identity.Principal, dataset string, id uuid.UUID) error {
	tx, err := s.begin(ctx, p, "approve")
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var creator uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `SELECT created_by,status FROM screening.private_imports WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3 FOR UPDATE`, p.TenantID, dataset, id).Scan(&creator, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if creator.String() == p.Subject {
		return identity.Forbidden()
	}
	if status == "rejected" {
		return nil
	}
	if status != "pending_review" {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE screening.private_imports SET status='rejected',reviewed_by=$4,reviewed_at=now() WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3`, p.TenantID, dataset, id, p.Subject); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.private_list_audit(tenant_id,dataset_id,import_id,actor,action) VALUES($1,$2,$3,$4,'rejected')`, p.TenantID, dataset, id, p.Subject); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Recover is scoped and explicit; it cannot fail a live staging lease or assume
// a human's expired session. The retained CSV/receipt can be re-uploaded by a
// current authorized user. Active/published evidence is untouched.
func (s Service) Recover(ctx context.Context, p identity.Principal, dataset string) (int64, error) {
	tx, err := s.begin(ctx, p, "import")
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var recovered int64
	err = tx.QueryRow(ctx, `WITH interrupted AS(SELECT i.id FROM screening.private_imports i LEFT JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(i.tenant_id,i.dataset_id,i.generation) WHERE i.tenant_id=$1 AND i.dataset_id=$2 AND i.status='staging' AND i.created_at<now()-interval '10 minutes' AND (v.id IS NULL OR v.status='failed' OR v.lease_until<=now()) ORDER BY i.created_at,i.id FOR UPDATE OF i SKIP LOCKED LIMIT 100), recovered AS(UPDATE screening.private_imports i SET status='failed',diagnostic='interrupted_staging_recovered' FROM interrupted x WHERE i.id=x.id RETURNING i.tenant_id,i.dataset_id,i.generation), versions AS(UPDATE screening.source_versions v SET status='failed' FROM recovered r WHERE(v.owner_scope,v.dataset_id,v.id)=(r.tenant_id,r.dataset_id,r.generation) AND v.status IN('staging','staged') AND v.lease_until<=now() RETURNING v.id) SELECT count(*) FROM recovered`, p.TenantID, dataset).Scan(&recovered)
	if err != nil {
		return 0, err
	}
	detail, _ := json.Marshal(map[string]any{"recovered": recovered, "observed_at": time.Now().UTC()})
	if _, err = tx.Exec(ctx, `INSERT INTO screening.private_list_audit(tenant_id,dataset_id,actor,action,detail) VALUES($1,$2,$3,'staging-recovery',$4)`, p.TenantID, dataset, p.Subject, detail); err != nil {
		return 0, err
	}
	return recovered, tx.Commit(ctx)
}
