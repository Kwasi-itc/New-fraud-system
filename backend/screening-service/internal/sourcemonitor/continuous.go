package sourcemonitor

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FanoutContinuousPage re-reads live ingestion objects through the existing
// continuous worker. Publication/receipt/object state/queue changes are atomic;
// retries and process death cannot skip a page. This is separate from immutable
// manual-query snapshots and never rewrites saved decision-time evidence.
func (s Service) FanoutContinuousPage(ctx context.Context) (bool, error) {
	if s.MonitoredEnqueue == nil {
		return false, nil
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	var publication int64
	var after uuid.UUID
	e = tx.QueryRow(ctx, `SELECT p.id FROM screening.source_publications p LEFT JOIN screening.source_continuous_delivery d ON d.publication_id=p.id WHERE d.completed_at IS NULL ORDER BY p.id FOR UPDATE OF p SKIP LOCKED LIMIT 1`).Scan(&publication)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO screening.source_continuous_delivery(publication_id) VALUES($1) ON CONFLICT DO NOTHING`, publication); e != nil {
		return false, e
	}
	if e = tx.QueryRow(ctx, `SELECT after_object FROM screening.source_continuous_delivery WHERE publication_id=$1 FOR UPDATE`, publication).Scan(&after); e != nil {
		return false, e
	}
	rows, e := tx.Query(ctx, `SELECT o.tenant_id,o.config_id,o.id FROM screening.source_publications p
 JOIN screening.source_datasets ds ON(ds.owner_scope,ds.id,ds.active_generation)=(p.owner_scope,p.dataset_id,p.generation)
 JOIN screening.continuous_screening_configs c ON c.provider='internal-watchlists' AND c.enabled AND c.field_map_json->>'version'='2'
 AND (p.owner_scope='00000000-0000-0000-0000-000000000000' OR p.owner_scope=c.tenant_id)
 AND (c.field_map_json->'provider_config'->'dataset_selection') @> jsonb_build_array(jsonb_build_object('scope',CASE WHEN p.owner_scope='00000000-0000-0000-0000-000000000000' THEN 'global' ELSE 'tenant' END,'id',p.dataset_id))
 JOIN screening.monitored_objects o ON(o.tenant_id,o.config_id,o.object_type)=(c.tenant_id,c.id,c.object_type)
 WHERE p.id=$1 AND o.id>$2 AND o.status IN('active','pending') ORDER BY o.id LIMIT 200 FOR UPDATE OF o`, publication, after)
	if e != nil {
		return false, e
	}
	type object struct{ tenant, config, id uuid.UUID }
	items := []object{}
	for rows.Next() {
		var o object
		if e = rows.Scan(&o.tenant, &o.config, &o.id); e != nil {
			rows.Close()
			return false, e
		}
		items = append(items, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false, e
	}
	for _, o := range items {
		tag, e := tx.Exec(ctx, `INSERT INTO screening.source_continuous_receipts(publication_id,tenant_id,config_id,monitored_object_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, publication, o.tenant, o.config, o.id)
		if e != nil {
			return false, e
		}
		if tag.RowsAffected() > 0 {
			if _, e = tx.Exec(ctx, `UPDATE screening.monitored_objects SET status='pending',updated_at=now() WHERE tenant_id=$1 AND id=$2`, o.tenant, o.id); e != nil {
				return false, e
			}
			if e = s.MonitoredEnqueue.EnqueueTx(ctx, tx, o.tenant.String(), o.id.String(), nil); e != nil {
				return false, e
			}
		}
		after = o.id
	}
	if _, e = tx.Exec(ctx, `UPDATE screening.source_continuous_delivery SET after_object=$2,completed_at=CASE WHEN $3<200 THEN now() END WHERE publication_id=$1`, publication, after, len(items)); e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}
