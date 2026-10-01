BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Migrate data-model, decision-engine and screening first: this is one database.
DO $$ BEGIN
  IF to_regclass('core.tenants') IS NULL OR to_regclass('core.decisions') IS NULL
    OR to_regclass('screening.screening_files') IS NULL THEN
    RAISE EXCEPTION 'Apply data-model, decision-engine and screening migrations before case-manager 000003';
  END IF;
  IF EXISTS (SELECT 1 FROM case_manager.case_decisions cd LEFT JOIN core.decisions d
    ON d.id=cd.decision_id AND d.tenant_id=cd.tenant_id WHERE d.id IS NULL) THEN
    RAISE EXCEPTION 'Decision ownership preflight failed; repair missing or cross-tenant references';
  END IF;
  IF EXISTS (SELECT 1 FROM case_manager.case_screenings cs LEFT JOIN screening.screenings s
    ON s.id=cs.screening_id AND s.tenant_id=cs.tenant_id WHERE s.id IS NULL) THEN
    RAISE EXCEPTION 'Screening ownership preflight failed; repair missing or cross-tenant references';
  END IF;
END $$;

ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_tenant_fk FOREIGN KEY(tenant_id) REFERENCES core.tenants(id);
ALTER TABLE case_manager.tags ADD CONSTRAINT tags_tenant_fk FOREIGN KEY(tenant_id) REFERENCES core.tenants(id);
ALTER TABLE case_manager.inbox_users ADD CONSTRAINT inbox_users_name_valid CHECK (btrim(user_id) <> '' AND user_id = btrim(user_id));
ALTER TABLE case_manager.case_files ADD COLUMN IF NOT EXISTS source_file_id UUID NULL;
CREATE UNIQUE INDEX case_files_source_unique ON case_manager.case_files(tenant_id,case_id,source_file_id) WHERE source_file_id IS NOT NULL;
CREATE INDEX case_decisions_lookup_idx ON case_manager.case_decisions(tenant_id,decision_id,case_id);
CREATE INDEX case_screenings_lookup_idx ON case_manager.case_screenings(tenant_id,screening_id,case_id);
CREATE INDEX inbox_users_access_idx ON case_manager.inbox_users(tenant_id,user_id,inbox_id);
CREATE INDEX cases_open_assignee_idx ON case_manager.cases(tenant_id,inbox_id,assigned_to) WHERE status <> 'closed';

CREATE TABLE IF NOT EXISTS case_manager.inbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  inbox_id UUID NOT NULL,
  actor_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);
ALTER TABLE case_manager.inbox_events ADD CONSTRAINT inbox_events_inbox_tenant_fk FOREIGN KEY(tenant_id,inbox_id) REFERENCES case_manager.inboxes(tenant_id,id);
CREATE INDEX IF NOT EXISTS inbox_events_history_idx ON case_manager.inbox_events(tenant_id,inbox_id,created_at DESC,id DESC);
COMMIT;
