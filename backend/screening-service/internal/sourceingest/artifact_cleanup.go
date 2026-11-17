package sourceingest

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
)

// Published, pending-review, ongoing and quarantined evidence is retained. Only
// explicitly aged, unreferenced failed/rejected artifacts can be removed, after
// the operator has lifted the per-dataset legal hold. Metadata/hashes survive.
func (s Store) CleanupArtifacts(ctx context.Context) (int64, error) {
	if s.Scope != nil {
		return 0, sourcedata.ErrPublish
	}
	actor, e := s.publisher(ctx)
	if e != nil {
		return 0, e
	}
	tag, e := s.DB.Exec(ctx, `WITH candidates AS(
 SELECT a.owner_scope,a.dataset_id,a.sha256 FROM screening.source_artifacts a JOIN screening.source_retention_policies p ON(p.owner_scope,p.dataset_id)=(a.owner_scope,a.dataset_id)
 WHERE (NOT $1 OR a.owner_scope='00000000-0000-0000-0000-000000000000') AND NOT p.legal_hold AND a.created_at<now()-p.failed_after
 AND NOT EXISTS(SELECT 1 FROM screening.source_retention_holds h WHERE(h.owner_scope,h.dataset_id,h.sha256)=(a.owner_scope,a.dataset_id,a.sha256))
 AND NOT EXISTS(SELECT 1 FROM screening.source_version_artifacts v WHERE(v.owner_scope,v.dataset_id,v.sha256)=(a.owner_scope,a.dataset_id,a.sha256))
 AND NOT EXISTS(SELECT 1 FROM screening.private_imports i WHERE(i.tenant_id,i.dataset_id,i.artifact_sha256)=(a.owner_scope,a.dataset_id,a.sha256) AND i.status IN ('staging','pending_review','published'))
 AND NOT EXISTS(SELECT 1 FROM screening.source_import_runs r WHERE(r.owner_scope,r.dataset_id,r.artifact_sha256)=(a.owner_scope,a.dataset_id,a.sha256) AND r.status<>'failed')
 ORDER BY a.created_at,a.owner_scope,a.dataset_id,a.sha256 LIMIT 100 FOR UPDATE OF a SKIP LOCKED),deleted AS(
 DELETE FROM screening.source_artifacts a USING candidates c WHERE(a.owner_scope,a.dataset_id,a.sha256)=(c.owner_scope,c.dataset_id,c.sha256) RETURNING a.owner_scope,a.dataset_id,a.sha256)
 INSERT INTO screening.source_lifecycle_audit(owner_scope,dataset_id,action,actor,detail) SELECT owner_scope,dataset_id,'artifact-expired',$2,jsonb_build_object('sha256',sha256) FROM deleted`, s.GlobalOnly, actor)
	return tag.RowsAffected(), e
}
