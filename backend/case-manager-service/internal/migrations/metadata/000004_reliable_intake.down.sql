BEGIN;
SET LOCAL lock_timeout = '5s';
ALTER TABLE case_manager.intake_receipts DROP CONSTRAINT intake_receipts_case_fk;
-- Refuse a lossy downgrade rather than deleting match evidence.
CREATE UNIQUE INDEX case_screenings_case_screening_idx ON case_manager.case_screenings(tenant_id,case_id,screening_id);
DROP INDEX case_manager.case_screenings_match_unique;
DROP INDEX case_manager.case_screenings_legacy_unique;
DROP INDEX case_manager.case_decisions_pivot_case_idx;
-- Receipts are retained: application rollback must not forget delivered events.
COMMIT;
