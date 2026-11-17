BEGIN;
SET LOCAL lock_timeout='5s';
DROP TABLE screening.source_retention_holds,screening.reviewed_notice_audit,screening.reviewed_notice_imports,screening.reviewed_notice_datasets;
-- Source publications, source artifacts and historical results are retained.
COMMIT;
