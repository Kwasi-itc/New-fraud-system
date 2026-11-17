BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Screening reads are latency-sensitive while full snapshots insert many names.
-- Avoid rescanning GIN's pending list on every request during bulk ingestion.
-- This trades slower collector writes for predictable indexed reads. Existing
-- pending entries must be flushed too; the setting alone does not remove them.
LOCK TABLE screening.source_names IN SHARE MODE;
SELECT pg_catalog.gin_clean_pending_list('screening.source_names_fuzzy_lookup'::regclass);
ALTER INDEX screening.source_names_fuzzy_lookup SET(fastupdate=off);
COMMIT;
