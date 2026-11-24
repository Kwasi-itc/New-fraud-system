package sourcemonitor

import (
	"context"
	"encoding/json"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
)

// Currentness is derived from actual result generations on every read. It does
// not wait for fanout or mistake an older successful clean result for current.
const currentSQL = `s.completed_at IS NOT NULL AND s.status<>'failed' AND NOT s.partial AND NOT EXISTS(
 SELECT 1 FROM screening.source_monitor_datasets d
 JOIN screening.source_datasets ds ON (ds.owner_scope,ds.id)=(d.owner_scope,d.dataset_id)
 LEFT JOIN screening.source_versions current_version ON(current_version.owner_scope,current_version.dataset_id,current_version.id)=(ds.owner_scope,ds.id,ds.active_generation)
 LEFT JOIN screening.source_readiness_v1 readiness ON (readiness.owner_scope,readiness.dataset_id)=(d.owner_scope,d.dataset_id)
 LEFT JOIN screening.source_grants g ON (g.tenant_id,g.owner_scope,g.dataset_id)=(m.tenant_id,d.owner_scope,d.dataset_id)
 WHERE d.monitor_id=m.id AND (NOT ds.enabled OR current_version.normalizer<>'` + sourcedata.Normalizer + `' OR (readiness.connector IS NOT NULL AND NOT readiness.ready) OR g.tenant_id IS NULL OR ds.active_generation IS NULL OR NOT EXISTS(
  SELECT 1 FROM jsonb_array_elements(coalesce(s.response_json->'datasets','[]')) evidence
  WHERE evidence->'ref'->>'id'=d.dataset_id AND evidence->'ref'->>'scope'=CASE WHEN d.owner_scope='00000000-0000-0000-0000-000000000000' THEN 'global' ELSE 'tenant' END
   AND evidence->>'generation'=ds.active_generation::text)))`

func (s Service) View(ctx context.Context, p identity.Principal, origin uuid.UUID) (json.RawMessage, error) {
	return s.ViewPage(ctx, p, origin, uuid.Nil)
}
func (s Service) ViewPage(ctx context.Context, p identity.Principal, origin, after uuid.UUID) (json.RawMessage, error) {
	if !p.IsHuman() || !p.Can("screening:search") {
		return nil, identity.Forbidden()
	}
	if after != uuid.Nil {
		var allowed bool
		if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.source_rescreen_targets t JOIN screening.source_monitors m ON(m.tenant_id,m.id)=(t.tenant_id,t.monitor_id) WHERE m.tenant_id=$1 AND m.origin_screening_id=$2 AND m.created_by=$3 AND t.id=$4)`, p.TenantID, origin, p.Subject, after).Scan(&allowed); err != nil {
			return nil, err
		} else if !allowed {
			return nil, sourcedata.ErrCursor
		}
	}
	var raw []byte
	err := s.DB.QueryRow(ctx, `WITH page AS MATERIALIZED(SELECT t.* FROM screening.source_rescreen_targets t JOIN screening.source_monitors m ON(m.tenant_id,m.id)=(t.tenant_id,t.monitor_id) WHERE m.tenant_id=$1 AND m.origin_screening_id=$2 AND m.created_by=$3 AND ($4='00000000-0000-0000-0000-000000000000'::uuid OR (t.created_at,t.id)<(SELECT created_at,id FROM screening.source_rescreen_targets WHERE id=$4)) ORDER BY t.created_at DESC,t.id DESC LIMIT 51)
 SELECT jsonb_build_object('id',m.id,'enabled',m.enabled,'current',coalesce((`+currentSQL+`),false),'latest_screening_id',m.latest_screening_id,
 'pending',(SELECT count(*) FROM screening.source_rescreen_targets t WHERE t.monitor_id=m.id AND t.status IN ('pending','queued')),
 'failed',(SELECT count(*) FROM screening.source_rescreen_targets t WHERE t.monitor_id=m.id AND t.status IN ('failed','cancelled')),
 'history_cursor',CASE WHEN (SELECT count(*) FROM page)>50 THEN (SELECT id::text FROM page ORDER BY created_at DESC,id DESC OFFSET 49 LIMIT 1) END,
 'history',coalesce((SELECT jsonb_agg(entry ORDER BY created_at DESC,id DESC) FROM (SELECT t.created_at,t.id,jsonb_build_object('id',t.id,'publication_id',t.publication_id,'status',t.status,'attempts',t.attempts,'screening_id',t.screening_id,'created_at',t.created_at) entry FROM page t ORDER BY t.created_at DESC,t.id DESC LIMIT 50) recent),'[]'))
 FROM screening.source_monitors m LEFT JOIN screening.screenings s ON (s.tenant_id,s.id)=(m.tenant_id,m.latest_screening_id)
 WHERE m.tenant_id=$1 AND m.origin_screening_id=$2 AND m.created_by=$3`, p.TenantID, origin, p.Subject, after).Scan(&raw)
	return raw, err
}

func (s Service) SetEnabled(ctx context.Context, p identity.Principal, origin uuid.UUID, enabled bool) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckIdentity(ctx, tx, p, true); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE screening.source_monitors SET enabled=$4,updated_at=now() WHERE tenant_id=$1 AND origin_screening_id=$2 AND created_by=$3`, p.TenantID, origin, p.Subject, enabled)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_monitor_audit(tenant_id,monitor_id,actor,action,detail) SELECT tenant_id,id,$3,'monitor-state',jsonb_build_object('enabled',$4::boolean) FROM screening.source_monitors WHERE tenant_id=$1 AND origin_screening_id=$2 AND created_by=$3`, p.TenantID, origin, p.Subject, enabled); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) Retry(ctx context.Context, p identity.Principal, origin, target uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = identity.CheckIdentity(ctx, tx, p, true); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE screening.source_rescreen_targets t SET status='pending',next_attempt_at=now(),updated_at=now() FROM screening.source_monitors m WHERE (m.tenant_id,m.id)=(t.tenant_id,t.monitor_id) AND m.enabled AND m.tenant_id=$1 AND m.origin_screening_id=$2 AND m.created_by=$3 AND t.id=$4 AND t.status IN ('failed','cancelled')`, p.TenantID, origin, p.Subject, target)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return identity.Forbidden()
	}
	if _, err = tx.Exec(ctx, `INSERT INTO screening.source_monitor_audit(tenant_id,monitor_id,actor,action,detail) SELECT tenant_id,id,$3,'retry',jsonb_build_object('target',$4::text) FROM screening.source_monitors WHERE tenant_id=$1 AND origin_screening_id=$2 AND created_by=$3`, p.TenantID, origin, p.Subject, target.String()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
