BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE INDEX investigator_reviews_case_history_idx
 ON screening.investigator_reviews(tenant_id,case_id,created_at DESC,operation_id DESC);
CREATE INDEX object_exclusion_applications_history_idx
 ON screening.object_exclusion_applications(tenant_id,operation_id,applied_at DESC,match_id DESC);
COMMIT;
