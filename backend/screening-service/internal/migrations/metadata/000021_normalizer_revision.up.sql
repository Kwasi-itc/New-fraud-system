BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- New writes are Unicode-17 projections. Existing immutable versions keep their
-- original marker; rebuilding requires a new publication, never an UPDATE of
-- historical records/names. A 304 cannot re-authorize an old projection.
ALTER TABLE screening.source_versions ALTER COLUMN normalizer SET DEFAULT 'nfkc-casefold-u17-v1';
CREATE OR REPLACE VIEW screening.source_readiness_v1 AS
 SELECT c.owner_scope,c.dataset_id,c.connector,c.rights_state,c.enabled,c.last_success_at,
 coalesce(c.rights_state='approved' AND c.last_success_at IS NOT NULL AND c.last_success_at>now()-c.max_stale AND c.last_error IN ('','source_download_failed') AND c.verified_generation=d.active_generation AND v.status='published' AND v.normalizer='nfkc-casefold-u17-v1',false) AS ready
 FROM screening.source_connections c JOIN screening.source_datasets d ON(d.owner_scope,d.id)=(c.owner_scope,c.dataset_id) LEFT JOIN screening.source_versions v ON(v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation);
COMMIT;
