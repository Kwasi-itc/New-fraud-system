BEGIN;
SET LOCAL lock_timeout='5s';
ALTER TABLE screening.source_versions ALTER COLUMN normalizer SET DEFAULT 'nfkc-casefold-v1';
CREATE OR REPLACE VIEW screening.source_readiness_v1 AS
 SELECT c.owner_scope,c.dataset_id,c.connector,c.rights_state,c.enabled,c.last_success_at,
 coalesce(c.rights_state='approved' AND c.last_success_at IS NOT NULL AND c.last_success_at>now()-c.max_stale AND c.last_error IN ('','source_download_failed') AND c.verified_generation=d.active_generation AND v.status='published',false) AS ready
 FROM screening.source_connections c JOIN screening.source_datasets d ON(d.owner_scope,d.id)=(c.owner_scope,c.dataset_id) LEFT JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation);
COMMIT;
