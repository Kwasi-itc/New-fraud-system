BEGIN;
SET LOCAL lock_timeout='5s';
DROP FUNCTION screening.refresh_source_statistics_v1();
COMMIT;
