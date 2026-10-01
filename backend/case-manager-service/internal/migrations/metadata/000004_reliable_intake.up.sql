BEGIN;
SET LOCAL lock_timeout = '5s';
CREATE TABLE IF NOT EXISTS case_manager.intake_receipts (
  tenant_id UUID NOT NULL,
  source TEXT NOT NULL,
  event_id UUID NOT NULL,
  payload_hash TEXT NOT NULL,
  case_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, source, event_id)
);
ALTER TABLE case_manager.intake_receipts ADD CONSTRAINT intake_receipts_case_fk FOREIGN KEY(tenant_id,case_id) REFERENCES case_manager.cases(tenant_id,id);
-- Preserve every match; the old uniqueness constraint silently merged matches.
DROP INDEX case_manager.case_screenings_case_screening_idx;
CREATE UNIQUE INDEX case_screenings_match_unique ON case_manager.case_screenings(tenant_id,case_id,screening_id,match_id) WHERE match_id IS NOT NULL;
CREATE UNIQUE INDEX case_screenings_legacy_unique ON case_manager.case_screenings(tenant_id,case_id,screening_id) WHERE match_id IS NULL;
CREATE INDEX case_decisions_pivot_case_idx ON case_manager.case_decisions(tenant_id,pivot_value,case_id);
COMMIT;
