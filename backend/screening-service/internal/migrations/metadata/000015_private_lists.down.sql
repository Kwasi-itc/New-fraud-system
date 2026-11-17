BEGIN;
SET LOCAL lock_timeout='5s';
DROP INDEX screening.source_rescreen_monitor_status;
DROP TABLE screening.private_list_audit,screening.private_imports,screening.private_lists;
DROP TABLE screening.database_identity;
-- Source publications, immutable generations and investigation evidence survive.
COMMIT;
