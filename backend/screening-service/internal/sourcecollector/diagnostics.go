package sourcecollector

import (
	"context"
	"encoding/json"
)

// Diagnostics is an operator-only aggregate read, not a public metrics route.
// No names, queries, tenant IDs, URLs, artifacts or credentials are returned.
func (c Collector) Diagnostics(ctx context.Context) (json.RawMessage, error) {
	var workflows bool
	if err := c.DB.QueryRow(ctx, `SELECT to_regclass('screening.screenings') IS NOT NULL`).Scan(&workflows); err != nil {
		return nil, err
	}
	workflowSignals := `jsonb_build_object('platform_workflows_supported',false,'publication_backlog',0,'rescreen_pending',0,'rescreen_queued',0,'rescreen_failed',0,'rescreen_retry_exhausted',0,'oldest_publication_seconds',0,'continuous_publication_backlog',0,'oldest_rescreen_seconds',0)`
	if workflows {
		workflowSignals = `jsonb_build_object(
 'platform_workflows_supported',true,
 'publication_backlog',(SELECT count(*) FROM screening.source_publication_delivery WHERE state='pending'),
 'rescreen_pending',(SELECT count(*) FROM screening.source_rescreen_targets WHERE status='pending'),
 'rescreen_queued',(SELECT count(*) FROM screening.source_rescreen_targets WHERE status='queued'),
 'rescreen_failed',(SELECT count(*) FROM screening.source_rescreen_targets WHERE status='failed'),
 'rescreen_retry_exhausted',(SELECT count(*) FROM screening.source_rescreen_targets WHERE status='failed' AND attempts>=5),
 'oldest_publication_seconds',coalesce((SELECT extract(epoch FROM now()-min(p.created_at))::bigint FROM screening.source_publication_delivery d JOIN screening.source_publications p ON p.id=d.publication_id WHERE d.state='pending'),0),
 'continuous_publication_backlog',(SELECT count(*) FROM screening.source_publications p LEFT JOIN screening.source_continuous_delivery d ON d.publication_id=p.id WHERE d.completed_at IS NULL),
 'oldest_rescreen_seconds',coalesce((SELECT extract(epoch FROM now()-min(created_at))::bigint FROM screening.source_rescreen_targets WHERE status IN ('pending','queued')),0))`
	}
	var raw []byte
	err := c.DB.QueryRow(ctx, `SELECT jsonb_build_object(
 'sources',(SELECT count(*) FROM screening.source_connections),
 'collection_enabled',(SELECT count(*) FROM screening.source_connections WHERE enabled),
 'ready',(SELECT count(*) FROM screening.source_readiness_v1 WHERE ready),
 'enabled_not_ready',(SELECT count(*) FROM screening.source_connections c LEFT JOIN screening.source_readiness_v1 r ON(r.owner_scope,r.dataset_id)=(c.owner_scope,c.dataset_id) WHERE c.enabled AND NOT coalesce(r.ready,false)),
 'overdue',(SELECT count(*) FROM screening.source_connections WHERE enabled AND next_check_at<now()),
 'quarantined',(SELECT count(*) FROM screening.source_connections WHERE last_error LIKE 'quarantined_%'),
 'expired_source_leases',(SELECT count(*) FROM screening.source_connections WHERE lease_until<=now()),
 'unpublished_staging',(SELECT count(*) FROM screening.source_versions WHERE status='staging'),
 'source_retry_exhausted',(SELECT count(*) FROM screening.source_connections WHERE enabled AND failures>=5),
 'attempts_24h',(SELECT count(*) FROM screening.source_import_runs WHERE started_at>now()-interval '1 day'),
 'failures_24h',(SELECT count(*) FROM screening.source_import_runs WHERE status IN ('failed','quarantined') AND started_at>now()-interval '1 day'),
 'statistics_refreshed_at',(SELECT min(last_analyze) FROM pg_stat_all_tables WHERE schemaname='screening' AND relname IN ('source_names','source_records')),
 'observed_at',now())`+` || `+workflowSignals).Scan(&raw)
	return raw, err
}
