package reviewednotices

import (
	"context"
	"encoding/json"
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s Service) Inspect(ctx context.Context, p identity.Principal, id uuid.UUID, after string) (json.RawMessage, error) {
	item, e := s.Get(ctx, p, id)
	if e != nil {
		return nil, e
	}
	if len(after) > 200 {
		return nil, ErrInput
	}
	var metadata []byte
	if e = s.DB.QueryRow(ctx, `SELECT jsonb_build_object('receipt',to_jsonb(i)-'request_hash','source',d.source_key,'category',s.category,'rights_state',c.rights_state,'collection_enabled',c.enabled,'tenant_grants',(SELECT count(*) FROM screening.source_grants g WHERE(g.owner_scope,g.dataset_id)=(i.owner_scope,i.dataset_id))) FROM screening.reviewed_notice_imports i JOIN screening.reviewed_notice_datasets d ON d.dataset_id=i.dataset_id JOIN screening.source_datasets s ON(s.owner_scope,s.id)=(i.owner_scope,i.dataset_id) JOIN screening.source_connections c ON(c.owner_scope,c.dataset_id)=(i.owner_scope,i.dataset_id) WHERE i.id=$1`, id).Scan(&metadata); e != nil {
		return nil, e
	}
	var diff any
	if item.Generation != nil {
		var parent *uuid.UUID
		if e = s.DB.QueryRow(ctx, `SELECT parent_generation FROM screening.source_versions WHERE owner_scope=$1 AND dataset_id=$2 AND id=$3`, uuid.Nil, item.Dataset, *item.Generation).Scan(&parent); e != nil {
			return nil, e
		}
		from := uuid.Nil
		if parent != nil {
			from = *parent
		}
		diff, e = s.store(p).Diff(ctx, sourceingest.Key{Dataset: item.Dataset}, from, *item.Generation, after, 100)
		if e != nil {
			return nil, e
		}
	}
	return json.Marshal(map[string]any{"metadata": json.RawMessage(metadata), "diff": diff, "coverage": "Only supplied reviewed references; not a complete agency feed", "rights_approval_is_separate": true})
}
func (s Service) Artifact(ctx context.Context, p identity.Principal, id uuid.UUID, kind string) ([]byte, string, error) {
	item, e := s.Get(ctx, p, id)
	if e != nil {
		return nil, "", e
	}
	column := "attachment_sha256"
	if kind == "csv" {
		column = "csv_sha256"
	} else if kind != "attachment" {
		return nil, "", ErrInput
	}
	var raw []byte
	var hash string
	e = s.DB.QueryRow(ctx, `SELECT a.body,a.sha256 FROM screening.reviewed_notice_imports i JOIN screening.source_artifacts a ON(a.owner_scope,a.dataset_id,a.sha256)=(i.owner_scope,i.dataset_id,i.`+column+`) WHERE i.id=$1`, id).Scan(&raw, &hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if e != nil {
		return nil, "", e
	}
	if _, e = s.DB.Exec(ctx, `INSERT INTO screening.reviewed_notice_audit(import_id,dataset_id,actor,actor_tenant,action,detail) VALUES($1,$2,$3,$4,'evidence-exported',jsonb_build_object('kind',$5::text,'sha256',$6::text))`, id, item.Dataset, p.Subject, p.TenantID, kind, hash); e != nil {
		return nil, "", e
	}
	return raw, hash, nil
}

// Recover releases only unpublished, failed/interrupted review holds. It does
// not delete evidence, bypass a live lease or mutate an active generation.
func (s Service) Recover(ctx context.Context, p identity.Principal, id uuid.UUID) error {
	tx, e := s.begin(ctx, p)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var dataset string
	var eligible bool
	e = tx.QueryRow(ctx, `SELECT i.dataset_id,(i.status='failed' OR(i.status='staging' AND i.created_at<now()-interval '10 minutes' AND NOT EXISTS(SELECT 1 FROM screening.source_versions v WHERE(v.owner_scope,v.dataset_id,v.id)=(i.owner_scope,i.dataset_id,i.generation) AND v.status='staging' AND v.lease_until>now()))) FROM screening.reviewed_notice_imports i WHERE i.id=$1 FOR UPDATE`, id).Scan(&dataset, &eligible)
	if e != nil {
		return e
	}
	if !eligible {
		return ErrConflict
	}
	if _, e = tx.Exec(ctx, `UPDATE screening.reviewed_notice_imports SET status='failed' WHERE id=$1`, id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM screening.source_retention_holds WHERE reference_id=$1`, id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO screening.reviewed_notice_audit(import_id,dataset_id,actor,actor_tenant,action) VALUES($1,$2,$3,$4,'interrupted-import-recovered')`, id, dataset, p.Subject, p.TenantID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
