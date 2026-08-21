-- Intentionally irreversible: rolling back must not restore a write path that
-- the ingestion service no longer maintains.
SELECT 1;
